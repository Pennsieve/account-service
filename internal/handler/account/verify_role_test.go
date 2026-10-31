package account

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// fakeAssumer answers AssumeRole like a role whose trust policy requires
// requiredId (none when empty), failing the first failFirst calls as a role
// still propagating through IAM does.
type fakeAssumer struct {
	requiredId string
	failFirst  int
	calls      []*sts.AssumeRoleInput
}

func (f *fakeAssumer) AssumeRole(_ context.Context, in *sts.AssumeRoleInput, _ ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	f.calls = append(f.calls, in)
	if len(f.calls) <= f.failFirst {
		return nil, errors.New("AccessDenied (propagating)")
	}
	if f.requiredId != "" && (in.ExternalId == nil || *in.ExternalId != f.requiredId) {
		return nil, errors.New("AccessDenied")
	}
	return &sts.AssumeRoleOutput{}, nil
}

var noSleep = func(time.Duration) {}
var validId = "pn-" + strings.Repeat("a", 32)

func TestVerifyRole_WithExternalId(t *testing.T) {
	f := &fakeAssumer{requiredId: validId}
	if err := verifyRole(context.Background(), f, "111122223333", "Pennsieve-Compute-dev-1", validId, noSleep); err != nil {
		t.Fatalf("want ok, got %v", err)
	}
	if len(f.calls) != 2 || f.calls[1].ExternalId != nil {
		t.Fatalf("want a check with the id and one without, got %d calls", len(f.calls))
	}
	if got := *f.calls[0].RoleArn; got != "arn:aws:iam::111122223333:role/Pennsieve-Compute-dev-1" {
		t.Fatalf("role arn: %s", got)
	}
}

func TestVerifyRole_RetriesWhilePropagating(t *testing.T) {
	f := &fakeAssumer{requiredId: validId, failFirst: 3}
	var slept []time.Duration
	if err := verifyRole(context.Background(), f, "1", "r", validId, func(d time.Duration) { slept = append(slept, d) }); err != nil {
		t.Fatalf("want ok after retries, got %v", err)
	}
	if len(slept) != 3 {
		t.Fatalf("want 3 waits, got %v", slept)
	}
}

func TestVerifyRole_GivesUp(t *testing.T) {
	f := &fakeAssumer{requiredId: validId, failFirst: 100}
	if err := verifyRole(context.Background(), f, "1", "r", validId, noSleep); !errors.Is(err, ErrRoleUnusable) {
		t.Fatalf("want ErrRoleUnusable, got %v", err)
	}
	if len(f.calls) != len(verifyRetryDelays)+1 {
		t.Fatalf("want %d attempts, got %d", len(verifyRetryDelays)+1, len(f.calls))
	}
}

func TestVerifyRole_TrustWithoutCondition(t *testing.T) {
	f := &fakeAssumer{} // the role trusts Pennsieve unconditionally
	if err := verifyRole(context.Background(), f, "1", "r", validId, noSleep); !errors.Is(err, ErrRoleNotRestricted) {
		t.Fatalf("want ErrRoleNotRestricted, got %v", err)
	}
}

func TestVerifyRole_WrongExternalId(t *testing.T) {
	f := &fakeAssumer{requiredId: "pn-" + strings.Repeat("b", 32)}
	if err := verifyRole(context.Background(), f, "1", "r", validId, noSleep); !errors.Is(err, ErrRoleUnusable) {
		t.Fatalf("want ErrRoleUnusable, got %v", err)
	}
}

func TestVerifyRole_Legacy(t *testing.T) {
	f := &fakeAssumer{}
	if err := verifyRole(context.Background(), f, "1", "r", "", noSleep); err != nil {
		t.Fatalf("want ok, got %v", err)
	}
	if len(f.calls) != 1 || f.calls[0].ExternalId != nil {
		t.Fatalf("legacy: want one call without an external id, got %d", len(f.calls))
	}
}

func TestVerifyRetryDelaysFitTheGateway(t *testing.T) {
	var total time.Duration
	for _, d := range verifyRetryDelays {
		total += d
	}
	if total > 20*time.Second {
		t.Fatalf("retries take %v; API Gateway stops waiting at 30s", total)
	}
}

func TestValidExternalIdAllowsLegacy(t *testing.T) {
	if !validExternalId("") || !validExternalId(validId) || validExternalId("ext-123") {
		t.Fatal("empty and issued ids are valid, anything else isn't")
	}
}

// Registered from template v2, an account has a runtime role next to its
// provisioning role: both are checked, with the external ID and without.
func TestVerifyRoles_TemplateV2ChecksBoth(t *testing.T) {
	f := &fakeAssumer{requiredId: validId}
	runtime, err := verifyRoles(context.Background(), f, "111122223333", "Pennsieve-Compute-dev-1", validId, "2", noSleep)
	if err != nil {
		t.Fatalf("want ok, got %v", err)
	}
	if runtime != "Pennsieve-Compute-dev-1-runtime" {
		t.Fatalf("runtime role: %q", runtime)
	}
	if len(f.calls) != 4 {
		t.Fatalf("want 2 checks per role, got %d calls", len(f.calls))
	}
	if got := *f.calls[2].RoleArn; got != "arn:aws:iam::111122223333:role/Pennsieve-Compute-dev-1-runtime" {
		t.Fatalf("runtime role arn: %s", got)
	}
	if f.calls[2].ExternalId == nil || f.calls[3].ExternalId != nil {
		t.Fatal("want the runtime role checked with the id, then without")
	}
}

// Before template v2 (or without one, for a legacy role) there is one role.
func TestVerifyRoles_OneRoleBeforeV2(t *testing.T) {
	for _, version := range []string{"", "1"} {
		f := &fakeAssumer{requiredId: validId}
		runtime, err := verifyRoles(context.Background(), f, "1", "r", validId, version, noSleep)
		if err != nil || runtime != "" {
			t.Fatalf("version %q: want no runtime role, got %q, %v", version, runtime, err)
		}
		if len(f.calls) != 2 {
			t.Fatalf("version %q: want 2 calls, got %d", version, len(f.calls))
		}
	}
}

// The runtime role gets fewer waits: it was made by the same stack, just
// checked, and the request has to stay inside API Gateway's limit.
func TestVerifyRoles_RuntimeRoleGivesUpSooner(t *testing.T) {
	slow := &slowRuntimeRole{fakeAssumer: fakeAssumer{requiredId: validId}, failFirst: len(runtimeRoleRetryDelays) + 1}
	var slept int
	_, err := verifyRoles(context.Background(), slow, "1", "r", validId, "2", func(time.Duration) { slept++ })
	if !errors.Is(err, ErrRoleUnusable) {
		t.Fatalf("want ErrRoleUnusable, got %v", err)
	}
	if slept != len(runtimeRoleRetryDelays) {
		t.Fatalf("want %d waits, got %d", len(runtimeRoleRetryDelays), slept)
	}

	slow = &slowRuntimeRole{fakeAssumer: fakeAssumer{requiredId: validId}, failFirst: len(runtimeRoleRetryDelays)}
	if _, err := verifyRoles(context.Background(), slow, "1", "r", validId, "2", noSleep); err != nil {
		t.Fatalf("want ok within the waits, got %v", err)
	}
}

// slowRuntimeRole is a fakeAssumer whose runtime role fails its first
// failFirst calls, as one still propagating does.
type slowRuntimeRole struct {
	fakeAssumer
	failFirst int
	runtime   int
}

func (s *slowRuntimeRole) AssumeRole(ctx context.Context, in *sts.AssumeRoleInput, opts ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	if in.RoleArn != nil && strings.HasSuffix(*in.RoleArn, "-runtime") {
		s.runtime++
		if s.runtime <= s.failFirst {
			return nil, errors.New("AccessDenied (propagating)")
		}
	}
	return s.fakeAssumer.AssumeRole(ctx, in, opts...)
}

func TestRuntimeRoleFor(t *testing.T) {
	for version, want := range map[string]string{"": "", "1": "", "2": "r-runtime", "3": "r-runtime", "x": ""} {
		if got := runtimeRoleFor("r", version); got != want {
			t.Errorf("version %q: got %q, want %q", version, got, want)
		}
	}
}
