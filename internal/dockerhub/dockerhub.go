// Package dockerhub resolves the latest released version tag for the private
// provisioner images on Docker Hub. The GET /compute-nodes endpoint uses it to
// tell the frontend whether a node's pinned provisioner tag is behind the
// newest real vX.Y.Z release.
//
// Docker Hub repos are private, so we authenticate with the same credentials the
// ECS provisioner uses (a Secrets Manager secret holding {"username","password"})
// via the registry v2 token flow. Results are cached in-process so the
// frequently-called list endpoint stays fast and we don't hit Docker Hub rate
// limits; on a lookup error we serve the last good (stale) value rather than
// failing the caller.
package dockerhub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

const (
	authURL     = "https://auth.docker.io/token"
	registryURL = "https://registry-1.docker.io"
	// defaultTTL bounds how long a warm container reuses a tag before re-reading
	// the shared SSM cache. The scheduled refresher (health-checker) is the real
	// freshness driver; this just limits per-container SSM reads.
	defaultTTL = 5 * time.Minute

	// whitelistParam enumerates the provisioner images to track (shared with the
	// POST handler's image whitelist).
	whitelistParam = "/%s/account-service/provisioner-images-whitelist"
	// latestTagParam is the shared cache the refresher writes and GET reads.
	latestTagParam = "/%s/account-service/provisioner-latest-tag/%s"
)

// semverTag matches a real release tag: an optional leading "v" followed by
// MAJOR.MINOR.PATCH. Tags like "latest" or "v1.2" are intentionally excluded.
var semverTag = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// SecretsAPI is the subset of the Secrets Manager client used here.
type SecretsAPI interface {
	GetSecretValue(ctx context.Context, in *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

// SSMAPI is the subset of the SSM client used here. GetParameter backs the
// shared read cache; PutParameter is the refresher's write.
type SSMAPI interface {
	GetParameter(ctx context.Context, in *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error)
	PutParameter(ctx context.Context, in *ssm.PutParameterInput, optFns ...func(*ssm.Options)) (*ssm.PutParameterOutput, error)
}

// HTTPClient is the subset of *http.Client used here (for testing).
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type cacheEntry struct {
	latest    string
	fetchedAt time.Time
}

// Resolver resolves the latest semver tag per image across three tiers: an
// in-process map (L1), a shared SSM parameter (L2, written by the scheduled
// refresher), and a direct Docker Hub lookup (fallback / refresher source).
type Resolver struct {
	sm        SecretsAPI
	ssm       SSMAPI
	http      HTTPClient
	secretArn string
	env       string
	ttl       time.Duration
	now       func() time.Time

	mu    sync.Mutex
	cache map[string]cacheEntry
	creds *dockerCreds // resolved lazily, then reused
}

type dockerCreds struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// NewResolver builds a resolver. secretArn is the Secrets Manager secret holding
// the Docker Hub username/password; env is the deployment environment used to
// build SSM parameter paths.
func NewResolver(sm SecretsAPI, ssmClient SSMAPI, secretArn, env string) *Resolver {
	if env == "" {
		env = "dev"
	}
	return &Resolver{
		sm:        sm,
		ssm:       ssmClient,
		http:      &http.Client{Timeout: 10 * time.Second},
		secretArn: secretArn,
		env:       env,
		ttl:       defaultTTL,
		now:       time.Now,
		cache:     make(map[string]cacheEntry),
	}
}

// LatestVersion returns the highest real vX.Y.Z tag for image (e.g.
// "pennsieve/compute-node-aws-provisioner-v2") on the read path used by GET
// /compute-nodes. It checks the in-process cache, then the shared SSM cache, and
// only as a last resort queries Docker Hub directly (self-healing if the
// scheduled refresher hasn't populated SSM yet). An empty string means
// "unknown" — callers should treat that as "can't determine" and not flag.
func (r *Resolver) LatestVersion(ctx context.Context, image string) string {
	// L1: in-process cache.
	r.mu.Lock()
	entry, ok := r.cache[image]
	fresh := ok && r.now().Sub(entry.fetchedAt) < r.ttl
	r.mu.Unlock()
	if fresh {
		return entry.latest
	}

	// L2: shared SSM cache.
	if latest, ok := r.ssmGet(ctx, image); ok {
		r.store(image, latest)
		return latest
	}

	// Fallback: query Docker Hub directly and best-effort seed the shared cache.
	latest, err := r.fetchLatest(ctx, image)
	if err != nil {
		return entry.latest // serve stale (possibly "") rather than failing the caller
	}
	r.store(image, latest)
	r.ssmPut(ctx, image, latest)
	return latest
}

// Refresh queries Docker Hub for the latest tag and writes it to the shared SSM
// cache, updating the in-process cache too. This is the scheduled refresher's
// entry point (called from the health-checker). Returns the resolved tag.
func (r *Resolver) Refresh(ctx context.Context, image string) (string, error) {
	latest, err := r.fetchLatest(ctx, image)
	if err != nil {
		return "", err
	}
	if err := r.ssmPut(ctx, image, latest); err != nil {
		return latest, err
	}
	r.store(image, latest)
	return latest, nil
}

// RefreshAll refreshes every image in the provisioner whitelist. It refreshes
// all images even if some fail, returning the first error encountered.
func (r *Resolver) RefreshAll(ctx context.Context) error {
	images, err := r.whitelistImages(ctx)
	if err != nil {
		return err
	}
	var firstErr error
	for _, image := range images {
		if _, err := r.Refresh(ctx, image); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (r *Resolver) store(image, latest string) {
	r.mu.Lock()
	r.cache[image] = cacheEntry{latest: latest, fetchedAt: r.now()}
	r.mu.Unlock()
}

// ssmGet reads the cached latest tag for image. ok is false on a missing
// parameter, any error, or a value that isn't a real vX.Y.Z tag — the last case
// covers the Terraform-seeded "dummy" placeholder before the first refresh, so
// callers fall through to Docker Hub and self-heal rather than surfacing junk.
func (r *Resolver) ssmGet(ctx context.Context, image string) (string, bool) {
	out, err := r.ssm.GetParameter(ctx, &ssm.GetParameterInput{
		Name: aws.String(fmt.Sprintf(latestTagParam, r.env, image)),
	})
	if err != nil || out.Parameter == nil || out.Parameter.Value == nil {
		return "", false
	}
	value := *out.Parameter.Value
	if !semverTag.MatchString(value) {
		return "", false
	}
	return value, true
}

func (r *Resolver) ssmPut(ctx context.Context, image, latest string) error {
	_, err := r.ssm.PutParameter(ctx, &ssm.PutParameterInput{
		Name:      aws.String(fmt.Sprintf(latestTagParam, r.env, image)),
		Value:     aws.String(latest),
		Type:      ssmtypes.ParameterTypeString,
		Overwrite: aws.Bool(true),
	})
	if err != nil {
		return fmt.Errorf("write latest-tag cache for %s: %w", image, err)
	}
	return nil
}

func (r *Resolver) whitelistImages(ctx context.Context) ([]string, error) {
	out, err := r.ssm.GetParameter(ctx, &ssm.GetParameterInput{
		Name: aws.String(fmt.Sprintf(whitelistParam, r.env)),
	})
	if err != nil {
		return nil, fmt.Errorf("read provisioner whitelist: %w", err)
	}
	if out.Parameter == nil || out.Parameter.Value == nil {
		return nil, fmt.Errorf("provisioner whitelist parameter is empty")
	}
	var images []string
	for _, img := range strings.Split(*out.Parameter.Value, ",") {
		if trimmed := strings.TrimSpace(img); trimmed != "" {
			images = append(images, trimmed)
		}
	}
	return images, nil
}

func (r *Resolver) fetchLatest(ctx context.Context, image string) (string, error) {
	creds, err := r.getCreds(ctx)
	if err != nil {
		return "", err
	}

	token, err := r.getToken(ctx, image, creds)
	if err != nil {
		return "", err
	}

	tags, err := r.listTags(ctx, image, token)
	if err != nil {
		return "", err
	}

	return highestSemver(tags), nil
}

func (r *Resolver) getCreds(ctx context.Context) (*dockerCreds, error) {
	r.mu.Lock()
	if r.creds != nil {
		c := r.creds
		r.mu.Unlock()
		return c, nil
	}
	r.mu.Unlock()

	if r.secretArn == "" {
		return nil, fmt.Errorf("docker hub credentials secret arn not configured")
	}

	out, err := r.sm.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(r.secretArn),
	})
	if err != nil {
		return nil, fmt.Errorf("get docker hub credentials: %w", err)
	}
	if out.SecretString == nil {
		return nil, fmt.Errorf("docker hub credentials secret is empty")
	}
	var creds dockerCreds
	if err := json.Unmarshal([]byte(*out.SecretString), &creds); err != nil {
		return nil, fmt.Errorf("unmarshal docker hub credentials: %w", err)
	}
	if creds.Username == "" || creds.Password == "" {
		return nil, fmt.Errorf("docker hub credentials missing username or password")
	}

	r.mu.Lock()
	r.creds = &creds
	r.mu.Unlock()
	return &creds, nil
}

func (r *Resolver) getToken(ctx context.Context, image string, creds *dockerCreds) (string, error) {
	q := url.Values{}
	q.Set("service", "registry.docker.io")
	q.Set("scope", fmt.Sprintf("repository:%s:pull", image))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, authURL+"?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	basic := base64.StdEncoding.EncodeToString([]byte(creds.Username + ":" + creds.Password))
	req.Header.Set("Authorization", "Basic "+basic)

	resp, err := r.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("request docker hub token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("docker hub token request returned %d", resp.StatusCode)
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode docker hub token: %w", err)
	}
	if body.Token == "" {
		return "", fmt.Errorf("docker hub token response empty")
	}
	return body.Token, nil
}

func (r *Resolver) listTags(ctx context.Context, image, token string) ([]string, error) {
	reqURL := fmt.Sprintf("%s/v2/%s/tags/list?n=1000", registryURL, image)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := r.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list docker hub tags: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("docker hub tags/list returned %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var body struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode docker hub tags: %w", err)
	}
	return body.Tags, nil
}

// highestSemver returns the highest vX.Y.Z tag (preserving its original "v"
// prefix form), or "" if none of the tags are real release versions.
func highestSemver(tags []string) string {
	type parsed struct {
		raw     string
		a, b, c int
	}
	var versions []parsed
	for _, t := range tags {
		m := semverTag.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		c, _ := strconv.Atoi(m[3])
		versions = append(versions, parsed{raw: t, a: a, b: b, c: c})
	}
	if len(versions) == 0 {
		return ""
	}
	sort.Slice(versions, func(i, j int) bool {
		if versions[i].a != versions[j].a {
			return versions[i].a > versions[j].a
		}
		if versions[i].b != versions[j].b {
			return versions[i].b > versions[j].b
		}
		return versions[i].c > versions[j].c
	})
	return versions[0].raw
}

// IsOutdated reports whether a node running currentTag should be nudged to
// update to latest. latest == "" means "unknown" → never flag. When the node's
// tag is an older semver it is outdated; per product decision, any non-semver
// tag (e.g. the floating "latest") is also flagged so nodes get pinned to a real
// release.
func IsOutdated(currentTag, latest string) bool {
	if latest == "" {
		return false
	}
	cur := semverTag.FindStringSubmatch(currentTag)
	if cur == nil {
		return true // floating / non-release tag — nudge to pin
	}
	lat := semverTag.FindStringSubmatch(latest)
	if lat == nil {
		return false
	}
	ca, _ := strconv.Atoi(cur[1])
	cb, _ := strconv.Atoi(cur[2])
	cc, _ := strconv.Atoi(cur[3])
	la, _ := strconv.Atoi(lat[1])
	lb, _ := strconv.Atoi(lat[2])
	lc, _ := strconv.Atoi(lat[3])
	if ca != la {
		return ca < la
	}
	if cb != lb {
		return cb < lb
	}
	return cc < lc
}
