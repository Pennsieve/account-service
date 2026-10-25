package compute

import (
	"context"
	"testing"
	"time"

	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/policies"
)

type recordingRoleCodes struct {
	store_dynamodb.RoleCodeStore
	issued []store_dynamodb.RoleCode
}

func (r *recordingRoleCodes) Issue(_ context.Context, c store_dynamodb.RoleCode) error {
	r.issued = append(r.issued, c)
	return nil
}

// The role setup's code is recorded as issued to the caller, for its role,
// for a day.
func TestIssueRoleCode(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	codes := &recordingRoleCodes{}
	resp := roleConfigResponse{RoleName: "Pennsieve-Compute-dev-1a2b3c4d", ExternalId: "pn-0123456789abcdef0123456789abcdef"}
	if err := issueRoleCode(context.Background(), codes, resp, "N:user:joost", now); err != nil {
		t.Fatal(err)
	}
	if len(codes.issued) != 1 {
		t.Fatalf("want one code issued, got %d", len(codes.issued))
	}
	got := codes.issued[0]
	want := store_dynamodb.RoleCode{
		ExternalId: resp.ExternalId, UserId: "N:user:joost", RoleName: resp.RoleName,
		IssuedAt: now.Unix(), ExpiresAt: now.Add(24 * time.Hour).Unix(),
	}
	if got != want {
		t.Fatalf("issued %+v, want %+v", got, want)
	}
}

// GET /role-policy serves the policy files: the provisioning policies merged
// (the same documents the CloudFormation template is rendered from) and the
// runtime policy byte for byte, with the runtime role's name.
func TestRolePolicyIsThePolicyFile(t *testing.T) {
	resp, err := buildRoleConfig("Pennsieve-Compute-dev-1a2b3c4d", "123456789012", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.PolicyDocument) != policies.ComputeRolePolicy {
		t.Fatal("the role-policy response's policyDocument differs from the provisioning policies merged")
	}
	if string(resp.RuntimePolicyDocument) != policies.RuntimeRolePolicy {
		t.Fatal("the role-policy response's runtimePolicyDocument differs from policies/compute-runtime-role-policy.json")
	}
	if resp.RuntimeRoleName != "Pennsieve-Compute-dev-1a2b3c4d-runtime" {
		t.Fatalf("runtimeRoleName: %s", resp.RuntimeRoleName)
	}
}
