package rolepolicy_test

import (
	"encoding/json"
	"testing"

	"github.com/pennsieve/account-service/internal/handler/compute"
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

func parsePolicy(t *testing.T) policyDocument {
	t.Helper()
	var doc policyDocument
	require.NoError(t, json.Unmarshal([]byte(compute.RolePolicyJSON()), &doc))
	return doc
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

func TestRolePolicyDocument_IsValidJSON(t *testing.T) {
	doc := parsePolicy(t)
	assert.Equal(t, "2012-10-17", doc.Version)
	assert.Len(t, doc.Statement, 6)
}

func TestRolePolicyDocument_CreateServiceLinkedRoleInAllowServices(t *testing.T) {
	doc := parsePolicy(t)
	stmt := findStatement(t, doc, "AllowServices")

	actions, ok := stmt.Action.([]interface{})
	require.True(t, ok)
	assert.Contains(t, actions, "iam:CreateServiceLinkedRole")
}

func TestRolePolicyDocument_AllExpectedStatementsPresent(t *testing.T) {
	doc := parsePolicy(t)

	expected := map[string]string{
		"AllowServices":                 "Allow",
		"AllowIAMRoleManagement":        "Allow",
		"AllowMetricReads":              "Allow",
		"DenyCreateRoleWithoutBoundary": "Deny",
		"DenyBoundaryStripping":         "Deny",
		"DenySelfModification":          "Deny",
	}

	found := make(map[string]string)
	for _, stmt := range doc.Statement {
		found[stmt.Sid] = stmt.Effect
	}

	for sid, wantEffect := range expected {
		gotEffect, ok := found[sid]
		assert.True(t, ok, "statement %q not found", sid)
		assert.Equal(t, wantEffect, gotEffect, "statement %q wrong effect", sid)
	}
}

func TestRolePolicyDocument_AutoscalingWildcardInAllowServices(t *testing.T) {
	doc := parsePolicy(t)
	stmt := findStatement(t, doc, "AllowServices")

	actions, ok := stmt.Action.([]interface{})
	require.True(t, ok)
	assert.Contains(t, actions, "autoscaling:*")
}

func TestRolePolicyDocument_TaggedResourceCreatesInAllowServices(t *testing.T) {
	doc := parsePolicy(t)
	stmt := findStatement(t, doc, "AllowServices")

	actions, ok := stmt.Action.([]interface{})
	require.True(t, ok)
	assert.Contains(t, actions, "events:PutRule")
	assert.Contains(t, actions, "events:TagResource")
	assert.Contains(t, actions, "bedrock:CreateGuardrail")
	assert.Contains(t, actions, "bedrock:TagResource")
}

func TestRolePolicyDocument_MetricReadsAreReadOnly(t *testing.T) {
	doc := parsePolicy(t)
	stmt := findStatement(t, doc, "AllowMetricReads")

	actions, ok := stmt.Action.([]interface{})
	require.True(t, ok)
	assert.ElementsMatch(t, []interface{}{
		"cloudwatch:ListMetrics",
		"cloudwatch:GetMetricData",
		"cloudwatch:GetMetricStatistics",
	}, actions, "metric reads must stay read-only — no PutMetricData, no alarm actions")
}

// A `cloudwatch:namespace` condition looks like tighter scoping and is the
// obvious "improvement" to make here. It is not: that key is only evaluated
// for publish actions, so on these three reads it is never satisfied and the
// statement denies everything instead of narrowing it (verified against the
// live API — with the condition, AWS/S3 reads return AccessDenied). This test
// exists so that change fails here rather than silently in production.
func TestRolePolicyDocument_MetricReadsCarryNoCondition(t *testing.T) {
	doc := parsePolicy(t)
	stmt := findStatement(t, doc, "AllowMetricReads")

	assert.Empty(t, stmt.Condition,
		"a cloudwatch:namespace condition is not evaluated for metric reads and denies them outright")
}

func TestRolePolicyDocument_DenySelfModification_ProtectsComputeRoles(t *testing.T) {
	doc := parsePolicy(t)
	stmt := findStatement(t, doc, "DenySelfModification")

	resources, ok := stmt.Resource.([]interface{})
	require.True(t, ok)
	assert.Contains(t, resources, "arn:aws:iam::*:role/Pennsieve-Compute-*")
	assert.Contains(t, resources, "arn:aws:iam::*:role/ROLE-*")
}
