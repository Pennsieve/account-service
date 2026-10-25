// Checks on the compute roles' permission policies (policies/*.json): the
// provisioning role's three managed policies, served merged as GET
// /role-policy's policyDocument, and the runtime role's inline policy. See
// docs/least-privilege-roles.md for what each statement is for.
package rolepolicy_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pennsieve/account-service/internal/handler/compute"
	"github.com/pennsieve/account-service/policies"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type policyStatement struct {
	Sid       string                                `json:"Sid"`
	Effect    string                                `json:"Effect"`
	Action    interface{}                           `json:"Action"`
	Resource  interface{}                           `json:"Resource"`
	Condition map[string]map[string]json.RawMessage `json:"Condition"`
}

type policyDocument struct {
	Version   string            `json:"Version"`
	Statement []policyStatement `json:"Statement"`
}

func parse(t *testing.T, doc string) policyDocument {
	t.Helper()
	var d policyDocument
	require.NoError(t, json.Unmarshal([]byte(doc), &d))
	assert.Equal(t, "2012-10-17", d.Version)
	return d
}

func findStatement(t *testing.T, doc policyDocument, sid string) policyStatement {
	t.Helper()
	for _, s := range doc.Statement {
		if s.Sid == sid {
			return s
		}
	}
	t.Fatalf("statement %q not found", sid)
	return policyStatement{}
}

func list(t *testing.T, v interface{}) []string {
	t.Helper()
	switch x := v.(type) {
	case string:
		return []string{x}
	case []interface{}:
		out := make([]string, 0, len(x))
		for _, e := range x {
			out = append(out, e.(string))
		}
		return out
	}
	t.Fatalf("neither a string nor a list: %v", v)
	return nil
}

// compact is a document's size as IAM counts it: without whitespace.
func compact(t *testing.T, doc string) int {
	t.Helper()
	var v any
	require.NoError(t, json.Unmarshal([]byte(doc), &v))
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return len(b)
}

// Each provisioning document fits a managed policy (6,144 characters), the
// runtime document an inline one (10,240), and no Sid repeats across the
// provisioning documents, which GET /role-policy merges.
func TestPolicies_FitIAMLimits(t *testing.T) {
	sids := map[string]bool{}
	for _, p := range policies.ProvisioningPolicies {
		assert.LessOrEqual(t, compact(t, p.Document), 6144, "provisioning policy %q exceeds a managed policy's limit", p.Name)
		for _, s := range parse(t, p.Document).Statement {
			assert.False(t, sids[s.Sid], "statement %q appears twice", s.Sid)
			sids[s.Sid] = true
		}
	}
	assert.LessOrEqual(t, compact(t, policies.RuntimeRolePolicy), 10240)
}

// GET /role-policy's policyDocument is every provisioning statement, in order.
func TestRolePolicyDocument_IsTheProvisioningPoliciesMerged(t *testing.T) {
	merged := parse(t, compute.RolePolicyJSON())
	var want []string
	for _, p := range policies.ProvisioningPolicies {
		for _, s := range parse(t, p.Document).Statement {
			want = append(want, s.Sid)
		}
	}
	var got []string
	for _, s := range merged.Statement {
		got = append(got, s.Sid)
	}
	assert.Equal(t, want, got)
}

// The guardrails against privilege escalation: a role is only created with
// a Pennsieve permissions boundary, boundaries stay, and neither compute
// role (the runtime role is Pennsieve-Compute-*-runtime) can be changed by
// the provisioning role.
func TestProvisioningPolicy_Guardrails(t *testing.T) {
	doc := parse(t, compute.RolePolicyJSON())

	boundary := findStatement(t, doc, "DenyCreateRoleWithoutBoundary")
	assert.Equal(t, "Deny", boundary.Effect)
	assert.Equal(t, []string{"iam:CreateRole"}, list(t, boundary.Action))
	assert.JSONEq(t, `"arn:aws:iam::*:policy/pennsieve-boundary-*"`, string(boundary.Condition["ArnNotLike"]["iam:PermissionsBoundary"]))

	stripping := findStatement(t, doc, "DenyBoundaryStripping")
	assert.Equal(t, "Deny", stripping.Effect)
	assert.Equal(t, []string{"iam:DeleteRolePermissionsBoundary"}, list(t, stripping.Action))

	self := findStatement(t, doc, "DenySelfModification")
	assert.Equal(t, "Deny", self.Effect)
	assert.Contains(t, list(t, self.Resource), "arn:aws:iam::*:role/Pennsieve-Compute-*")
	assert.Contains(t, list(t, self.Resource), "arn:aws:iam::*:role/ROLE-*")
	for _, a := range []string{"iam:PutRolePolicy", "iam:AttachRolePolicy", "iam:DetachRolePolicy", "iam:UpdateAssumeRolePolicy", "iam:DeleteRole", "iam:PutRolePermissionsBoundary"} {
		assert.Contains(t, list(t, self.Action), a)
	}

	// The roles terraform makes are passed only to the services that run a
	// node's parts, never to anything else.
	pass := findStatement(t, doc, "PassNodeRoles")
	assert.Equal(t, []string{"iam:PassRole"}, list(t, pass.Action))
	assert.NotContains(t, list(t, pass.Resource), "*")
	assert.Contains(t, string(pass.Condition["StringEquals"]["iam:PassedToService"]), "ecs-tasks.amazonaws.com")
	roles := findStatement(t, doc, "ManageNodeRoles")
	assert.NotContains(t, list(t, roles.Resource), "*")
}

// The provisioning role manages infrastructure; it doesn't read the data
// the infrastructure holds.
func TestProvisioningPolicy_NoDataAccess(t *testing.T) {
	doc := parse(t, compute.RolePolicyJSON())
	for _, s := range doc.Statement {
		if s.Effect != "Allow" {
			continue
		}
		for _, a := range list(t, s.Action) {
			for _, forbidden := range []string{"kms:Decrypt", "kms:Encrypt", "kms:GenerateDataKey", "elasticfilesystem:Client", "dynamodb:GetItem", "dynamodb:Query", "dynamodb:Scan", "secretsmanager:", "logs:GetLogEvents", "logs:FilterLogEvents", "s3:*", "iam:*", "ec2:*", "lambda:InvokeFunction"} {
				assert.False(t, strings.HasPrefix(a, forbidden) || a == forbidden, "statement %q allows %s", s.Sid, a)
			}
		}
		// Object reads and writes only in terraform's state buckets.
		for _, a := range list(t, s.Action) {
			if a == "s3:GetObject" || a == "s3:PutObject" {
				for _, r := range list(t, s.Resource) {
					assert.True(t, strings.Contains(r, "tfstate-"), "statement %q: %s on %s", s.Sid, a, r)
				}
			}
		}
	}
}

// The runtime role never touches IAM, never creates or destroys a node's
// infrastructure, and only reaches named resources: the compute gateway,
// a node's cluster, the shared interactive ingress and run results.
func TestRuntimePolicy_IsNarrow(t *testing.T) {
	doc := parse(t, policies.RuntimeRolePolicy)
	for _, s := range doc.Statement {
		assert.Equal(t, "Allow", s.Effect)
		for _, a := range list(t, s.Action) {
			assert.False(t, strings.HasPrefix(a, "iam:"), "statement %q: %s", s.Sid, a)
			assert.False(t, strings.HasSuffix(a, ":*") || strings.HasSuffix(a, "*"), "statement %q: %s", s.Sid, a)
			for _, forbidden := range []string{"Delete", "Terminate", "PutObject", "RunTask", "StartExecution", "Create"} {
				if strings.Contains(a, forbidden) {
					assert.Contains(t, []string{"elasticloadbalancing:CreateTargetGroup", "elasticloadbalancing:CreateRule"}, a, "statement %q: %s", s.Sid, a)
				}
			}
		}
	}

	gateway := findStatement(t, doc, "InvokeComputeGateway")
	assert.ElementsMatch(t, []string{"lambda:InvokeFunctionUrl", "lambda:InvokeFunction"}, list(t, gateway.Action))
	assert.Equal(t, []string{"arn:aws:lambda:*:*:function:compute-gateway-v2-*"}, list(t, gateway.Resource))

	tasks := findStatement(t, doc, "FindSessionTasks")
	assert.ElementsMatch(t, []string{"ecs:ListTasks", "ecs:DescribeTasks"}, list(t, tasks.Action))
	assert.JSONEq(t, `"arn:aws:ecs:*:*:cluster/compute-cluster-*"`, string(tasks.Condition["ArnLike"]["ecs:cluster"]))

	targets := findStatement(t, doc, "RegisterSessionTargets")
	assert.Equal(t, []string{"arn:aws:elasticloadbalancing:*:*:targetgroup/itg-*/*"}, list(t, targets.Resource))

	results := findStatement(t, doc, "ReadRunResults")
	assert.ElementsMatch(t, []string{"s3:GetObject", "s3:ListBucket"}, list(t, results.Action))
	for _, r := range list(t, results.Resource) {
		assert.True(t, strings.HasPrefix(r, "arn:aws:s3:::workflow-logs-"), r)
	}
}

// A `cloudwatch:namespace` condition looks like tighter scoping and is the
// obvious "improvement" to make here. It is not: that key is only evaluated
// for publish actions, so on these reads it is never satisfied and the
// statement denies everything instead of narrowing it (verified against the
// live API — with the condition, AWS/S3 reads return AccessDenied). This test
// exists so that change fails here rather than silently in production.
func TestRuntimePolicy_MetricReadsAreReadOnlyAndCarryNoCondition(t *testing.T) {
	doc := parse(t, policies.RuntimeRolePolicy)
	stmt := findStatement(t, doc, "ReadStorageMetrics")
	assert.ElementsMatch(t, []string{"cloudwatch:ListMetrics", "cloudwatch:GetMetricData"}, list(t, stmt.Action),
		"metric reads must stay read-only — no PutMetricData, no alarm actions")
	assert.Empty(t, stmt.Condition,
		"a cloudwatch:namespace condition is not evaluated for metric reads and denies them outright")
}

// The runtime role's name follows the provisioning role's, so the
// Pennsieve-side grants on Pennsieve-Compute-* cover it.
func TestRuntimeRoleName(t *testing.T) {
	assert.Equal(t, "Pennsieve-Compute-dev-1a2b3c4d-runtime", policies.RuntimeRoleName("Pennsieve-Compute-dev-1a2b3c4d"))
}
