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

func TestRolePolicyDocument_AllowServiceLinkedRoles(t *testing.T) {
	doc := parsePolicy(t)
	stmt := findStatement(t, doc, "AllowServiceLinkedRoles")

	assert.Equal(t, "Allow", stmt.Effect)
	assert.Equal(t, "iam:CreateServiceLinkedRole", stmt.Action)
	assert.Equal(t, "arn:aws:iam::*:role/aws-service-role/*", stmt.Resource)

	raw := stmt.Condition["StringEquals"]["iam:AWSServiceName"]
	var names []string
	require.NoError(t, json.Unmarshal(raw, &names))
	assert.Contains(t, names, "autoscaling.amazonaws.com")
	assert.Contains(t, names, "ecs.amazonaws.com")
}

func TestRolePolicyDocument_AllExpectedStatementsPresent(t *testing.T) {
	doc := parsePolicy(t)

	expected := map[string]string{
		"AllowServices":                 "Allow",
		"AllowIAMRoleManagement":        "Allow",
		"AllowServiceLinkedRoles":       "Allow",
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

func TestRolePolicyDocument_DenySelfModification_ProtectsComputeRoles(t *testing.T) {
	doc := parsePolicy(t)
	stmt := findStatement(t, doc, "DenySelfModification")

	resources, ok := stmt.Resource.([]interface{})
	require.True(t, ok)
	assert.Contains(t, resources, "arn:aws:iam::*:role/Pennsieve-Compute-*")
	assert.Contains(t, resources, "arn:aws:iam::*:role/ROLE-*")
}
