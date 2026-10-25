// Checks on the compute role's CloudFormation template, rendered the way
// terraform renders it (terraform/cloudformation.tf): the permission policy
// from policies/compute-role-policy.json as compact JSON, and the version.
package cloudformation_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/pennsieve/account-service/policies"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// render is terraform's replace(replace(file(template), "__ROLE_POLICY__",
// jsonencode(jsondecode(file(policy)))), "__TEMPLATE_VERSION__", version).
func render(t *testing.T, version string) string {
	t.Helper()
	tpl, err := os.ReadFile("pennsieve-compute-role.yaml")
	require.NoError(t, err)
	var policy any
	require.NoError(t, json.Unmarshal([]byte(policies.ComputeRolePolicy), &policy))
	compact, err := json.Marshal(policy)
	require.NoError(t, err)
	out := strings.ReplaceAll(string(tpl), "__ROLE_POLICY__", string(compact))
	return strings.ReplaceAll(out, "__TEMPLATE_VERSION__", version)
}

// at walks a decoded YAML document by mapping keys and sequence indexes.
func at(t *testing.T, n *yaml.Node, path ...any) *yaml.Node {
	t.Helper()
	if n.Kind == yaml.DocumentNode {
		n = n.Content[0]
	}
	for _, p := range path {
		switch k := p.(type) {
		case string:
			require.Equal(t, yaml.MappingNode, n.Kind, "expected a mapping at %q", k)
			var next *yaml.Node
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == k {
					next = n.Content[i+1]
				}
			}
			require.NotNil(t, next, "no %q", k)
			n = next
		case int:
			require.Equal(t, yaml.SequenceNode, n.Kind)
			require.Less(t, k, len(n.Content))
			n = n.Content[k]
		}
	}
	return n
}

func parse(t *testing.T, doc string) *yaml.Node {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(doc), &root))
	return &root
}

// The rendered template's inline policy is exactly the policy file, the one
// GET /role-policy serves.
func TestTemplatePolicyIsThePolicyFile(t *testing.T) {
	root := parse(t, render(t, "1"))
	role := at(t, root, "Resources", "PennsieveComputeRole")
	assert.Equal(t, "AWS::IAM::Role", at(t, role, "Type").Value)

	var rendered, file any
	require.NoError(t, at(t, role, "Properties", "Policies", 0, "PolicyDocument").Decode(&rendered))
	require.NoError(t, json.Unmarshal([]byte(policies.ComputeRolePolicy), &file))
	// Through JSON, so YAML's and JSON's number types compare alike.
	a, _ := json.Marshal(rendered)
	b, _ := json.Marshal(file)
	assert.JSONEq(t, string(b), string(a))

	name := at(t, role, "Properties", "Policies", 0, "PolicyName")
	assert.Equal(t, "!Sub", name.Tag)
	assert.Equal(t, "POLICY-${RoleName}", name.Value)
}

func TestTemplateRole(t *testing.T) {
	root := parse(t, render(t, "7"))
	props := at(t, root, "Resources", "PennsieveComputeRole", "Properties")
	assert.Equal(t, "RoleName", at(t, props, "RoleName").Value)
	assert.Equal(t, "!Ref", at(t, props, "RoleName").Tag)
	assert.Equal(t, "7200", at(t, props, "MaxSessionDuration").Value)

	trust := at(t, props, "AssumeRolePolicyDocument", "Statement", 0)
	assert.Equal(t, "Allow", at(t, trust, "Effect").Value)
	assert.Equal(t, "sts:AssumeRole", at(t, trust, "Action").Value)
	assert.Equal(t, "arn:aws:iam::${PennsieveAccountId}:root", at(t, trust, "Principal", "AWS").Value)
	condition := at(t, trust, "Condition", "StringEquals", "sts:ExternalId")
	assert.Equal(t, "!Ref", condition.Tag)
	assert.Equal(t, "ExternalId", condition.Value)

	for _, p := range []string{"RoleName", "ExternalId", "PennsieveAccountId"} {
		at(t, root, "Parameters", p)
	}
	at(t, root, "Outputs", "RoleArn")
	at(t, root, "Outputs", "AccountId")

	assert.Equal(t, "7", at(t, root, "Metadata", "Pennsieve", "TemplateVersion").Value)
	assert.Contains(t, at(t, root, "Description").Value, "v7")
}

// Every placeholder is filled when rendered.
func TestTemplatePlaceholders(t *testing.T) {
	tpl, err := os.ReadFile("pennsieve-compute-role.yaml")
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(tpl), "__ROLE_POLICY__"))
	assert.NotContains(t, render(t, "1"), "__")
}
