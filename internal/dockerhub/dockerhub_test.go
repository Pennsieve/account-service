package dockerhub

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

func TestHighestSemver(t *testing.T) {
	cases := []struct {
		name string
		tags []string
		want string
	}{
		{"empty", nil, ""},
		{"only floating", []string{"latest", "main", "dev"}, ""},
		{"picks highest", []string{"v1.2.0", "v1.10.1", "v1.9.9", "latest"}, "v1.10.1"},
		{"major beats minor", []string{"v1.99.0", "v2.0.0"}, "v2.0.0"},
		{"no v prefix", []string{"1.0.0", "2.3.4"}, "2.3.4"},
		{"ignores partial semver", []string{"v1.2", "v1.2.3", "v1.2.3.4"}, "v1.2.3"},
		{"preserves raw form", []string{"v3.1.0"}, "v3.1.0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := highestSemver(c.tags); got != c.want {
				t.Errorf("highestSemver(%v) = %q, want %q", c.tags, got, c.want)
			}
		})
	}
}

func TestIsOutdated(t *testing.T) {
	cases := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{"unknown latest never flags", "v1.0.0", "", false},
		{"older is outdated", "v1.0.0", "v1.2.0", true},
		{"equal not outdated", "v1.2.0", "v1.2.0", false},
		{"newer not outdated", "v2.0.0", "v1.2.0", false},
		{"floating latest flagged", "latest", "v1.2.0", true},
		{"empty tag flagged", "", "v1.2.0", true},
		{"v-prefix mismatch tolerated", "1.0.0", "v2.0.0", true},
		{"patch comparison", "v1.2.3", "v1.2.4", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsOutdated(c.current, c.latest); got != c.want {
				t.Errorf("IsOutdated(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
			}
		})
	}
}

type fakeSecrets struct {
	value string
	calls int
}

func (f *fakeSecrets) GetSecretValue(ctx context.Context, in *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	f.calls++
	return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(f.value)}, nil
}

type fakeHTTP struct {
	tokenCalls int
	tagCalls   int
	tags       []string
	failTags   bool
}

func (f *fakeHTTP) Do(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Host, "auth.docker.io") {
		f.tokenCalls++
		return jsonResp(`{"token":"abc"}`), nil
	}
	f.tagCalls++
	if f.failTags {
		return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("boom")), Header: make(http.Header)}, nil
	}
	quoted := make([]string, len(f.tags))
	for i, t := range f.tags {
		quoted[i] = fmt.Sprintf("%q", t)
	}
	return jsonResp(fmt.Sprintf(`{"tags":[%s]}`, strings.Join(quoted, ","))), nil
}

func jsonResp(body string) *http.Response {
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

// fakeSSM holds parameters in a map. A missing key returns ParameterNotFound so
// the read path falls through to Docker Hub.
type fakeSSM struct {
	params   map[string]string
	getCalls int
	putCalls int
}

func newFakeSSM() *fakeSSM { return &fakeSSM{params: map[string]string{}} }

func (f *fakeSSM) GetParameter(ctx context.Context, in *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
	f.getCalls++
	v, ok := f.params[aws.ToString(in.Name)]
	if !ok {
		return nil, &ssmtypes.ParameterNotFound{}
	}
	return &ssm.GetParameterOutput{Parameter: &ssmtypes.Parameter{Value: aws.String(v)}}, nil
}

func (f *fakeSSM) PutParameter(ctx context.Context, in *ssm.PutParameterInput, optFns ...func(*ssm.Options)) (*ssm.PutParameterOutput, error) {
	f.putCalls++
	f.params[aws.ToString(in.Name)] = aws.ToString(in.Value)
	return &ssm.PutParameterOutput{}, nil
}

func newTestResolver(sm SecretsAPI, s SSMAPI, h HTTPClient) *Resolver {
	r := NewResolver(sm, s, "arn:secret", "test")
	r.http = h
	return r
}

func TestLatestVersion_FallbackCachesAndSeedsSSM(t *testing.T) {
	sm := &fakeSecrets{value: `{"username":"u","password":"p"}`}
	s := newFakeSSM() // empty → read path falls through to Docker Hub
	h := &fakeHTTP{tags: []string{"v1.0.0", "v1.5.0", "latest"}}
	r := newTestResolver(sm, s, h)

	if got := r.LatestVersion(context.Background(), "pennsieve/img"); got != "v1.5.0" {
		t.Fatalf("first lookup = %q, want v1.5.0", got)
	}
	// Fallback should have seeded the shared SSM cache.
	if s.putCalls != 1 {
		t.Errorf("expected SSM seeded once, got %d puts", s.putCalls)
	}
	// Second call within TTL must hit L1 (no new HTTP tag calls).
	if got := r.LatestVersion(context.Background(), "pennsieve/img"); got != "v1.5.0" {
		t.Fatalf("cached lookup = %q, want v1.5.0", got)
	}
	if h.tagCalls != 1 {
		t.Errorf("expected 1 tag call (L1 cached), got %d", h.tagCalls)
	}

	// Expire L1 and make the Docker Hub refresh fail → serve stale value.
	r.mu.Lock()
	r.cache["pennsieve/img"] = cacheEntry{latest: "v1.5.0", fetchedAt: time.Time{}}
	r.mu.Unlock()
	h.failTags = true
	if got := r.LatestVersion(context.Background(), "pennsieve/img"); got != "v1.5.0" {
		t.Errorf("stale-on-error = %q, want v1.5.0", got)
	}
}

func TestLatestVersion_ReadsSharedSSMCache(t *testing.T) {
	sm := &fakeSecrets{value: `{"username":"u","password":"p"}`}
	s := newFakeSSM()
	s.params["/test/account-service/provisioner-latest-tag/pennsieve/img"] = "v2.3.4"
	h := &fakeHTTP{tags: []string{"v9.9.9"}} // must NOT be consulted

	r := newTestResolver(sm, s, h)
	if got := r.LatestVersion(context.Background(), "pennsieve/img"); got != "v2.3.4" {
		t.Fatalf("got %q, want v2.3.4 (from SSM)", got)
	}
	if h.tagCalls != 0 {
		t.Errorf("Docker Hub should not be hit when SSM has the value, got %d tag calls", h.tagCalls)
	}
}

func TestLatestVersion_IgnoresDummyPlaceholder(t *testing.T) {
	sm := &fakeSecrets{value: `{"username":"u","password":"p"}`}
	s := newFakeSSM()
	// Terraform seeds the param with "dummy" before the first refresh.
	s.params["/test/account-service/provisioner-latest-tag/pennsieve/img"] = "dummy"
	h := &fakeHTTP{tags: []string{"v1.4.0", "latest"}}

	r := newTestResolver(sm, s, h)
	if got := r.LatestVersion(context.Background(), "pennsieve/img"); got != "v1.4.0" {
		t.Fatalf("got %q, want v1.4.0 (dummy ignored, fell through to Docker Hub)", got)
	}
	if h.tagCalls != 1 {
		t.Errorf("expected Docker Hub fallback (1 tag call), got %d", h.tagCalls)
	}
}

func TestRefreshAll_WritesWhitelistedImages(t *testing.T) {
	sm := &fakeSecrets{value: `{"username":"u","password":"p"}`}
	s := newFakeSSM()
	s.params["/test/account-service/provisioner-images-whitelist"] = "pennsieve/a, pennsieve/b"
	h := &fakeHTTP{tags: []string{"v1.2.3", "latest"}}

	r := newTestResolver(sm, s, h)
	if err := r.RefreshAll(context.Background()); err != nil {
		t.Fatalf("RefreshAll: %v", err)
	}
	if got := s.params["/test/account-service/provisioner-latest-tag/pennsieve/a"]; got != "v1.2.3" {
		t.Errorf("image a cached = %q, want v1.2.3", got)
	}
	if got := s.params["/test/account-service/provisioner-latest-tag/pennsieve/b"]; got != "v1.2.3" {
		t.Errorf("image b cached = %q, want v1.2.3", got)
	}
}

func TestLatestVersion_NoCredsConfigured(t *testing.T) {
	r := NewResolver(&fakeSecrets{}, newFakeSSM(), "", "test")
	r.http = &fakeHTTP{tags: []string{"v1.0.0"}}
	if got := r.LatestVersion(context.Background(), "pennsieve/img"); got != "" {
		t.Errorf("expected empty (no creds), got %q", got)
	}
}
