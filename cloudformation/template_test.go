// Checks on the compute roles' CloudFormation template, rendered the way
// terraform renders it (terraform/cloudformation.tf): each permission policy
// from policies/*.json as compact JSON, and the version.
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

// placeholders maps each policy placeholder in the template to its file's
// document, as terraform's cfn_policies does.
func placeholders() map[string]string {
	p := map[string]string{"__RUNTIME_POLICY__": policies.RuntimeRolePolicy}
	for _, pol := range policies.ProvisioningPolicies {
		p["__PROVISIONING_"+strings.ToUpper(pol.Name)+"_POLICY__"] = pol.Document
	}
	return p
}

// render is terraform's replace(..., placeholder, jsonencode(jsondecode(file(policy))))
// for each policy, then of "__TEMPLATE_VERSION__" with the version.
func render(t *testing.T, version string) string {
	t.Helper()
	tpl, err := os.ReadFile("pennsieve-compute-role.yaml")
	require.NoError(t, err)
	out := string(tpl)
	for placeholder, doc := range placeholders() {
		var policy any
		require.NoError(t, json.Unmarshal([]byte(doc), &policy))
		compact, err := json.Marshal(policy)
		require.NoError(t, err)
		out = strings.ReplaceAll(out, placeholder, string(compact))
	}
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

// assertSameDocument compares a rendered policy with its file through JSON,
// so YAML's and JSON's number types compare alike.
func assertSameDocument(t *testing.T, rendered *yaml.Node, file string) {
	t.Helper()
	var got, want any
	require.NoError(t, rendered.Decode(&got))
	require.NoError(t, json.Unmarshal([]byte(file), &want))
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(want)
	assert.JSONEq(t, string(b), string(a))
}

// The provisioning role's managed policies are exactly the policy files, in
// order, named after the role; the role has no inline policy.
func TestTemplateProvisioningPoliciesAreTheFiles(t *testing.T) {
	root := parse(t, render(t, "2"))
	role := at(t, root, "Resources", "PennsieveComputeRole")
	assert.Equal(t, "AWS::IAM::Role", at(t, role, "Type").Value)
	arns := at(t, role, "Properties", "ManagedPolicyArns")
	require.Len(t, arns.Content, len(policies.ProvisioningPolicies))

	for i, pol := range policies.ProvisioningPolicies {
		assert.Equal(t, "!Ref", arns.Content[i].Tag)
		resource := at(t, root, "Resources", arns.Content[i].Value)
		assert.Equal(t, "AWS::IAM::ManagedPolicy", at(t, resource, "Type").Value)
		name := at(t, resource, "Properties", "ManagedPolicyName")
		assert.Equal(t, "!Sub", name.Tag)
		assert.Equal(t, "${RoleName}-"+pol.Name, name.Value)
		assertSameDocument(t, at(t, resource, "Properties", "PolicyDocument"), pol.Document)
	}

	var props map[string]any
	require.NoError(t, at(t, role, "Properties").Decode(&props))
	assert.NotContains(t, props, "Policies", "the provisioning role's policies are managed, not inline")
}

// The runtime role's inline policy is exactly the runtime policy file.
func TestTemplateRuntimePolicyIsTheFile(t *testing.T) {
	root := parse(t, render(t, "2"))
	role := at(t, root, "Resources", "PennsieveComputeRuntimeRole")
	assert.Equal(t, "AWS::IAM::Role", at(t, role, "Type").Value)
	assertSameDocument(t, at(t, role, "Properties", "Policies", 0, "PolicyDocument"), policies.RuntimeRolePolicy)
	name := at(t, role, "Properties", "Policies", 0, "PolicyName")
	assert.Equal(t, "!Sub", name.Tag)
	assert.Equal(t, "POLICY-${RoleName}-runtime", name.Value)
}

// Both roles trust Pennsieve's account with the registration's external ID
// only; the runtime role is named after the provisioning role, as
// policies.RuntimeRoleName names it.
func TestTemplateRoles(t *testing.T) {
	root := parse(t, render(t, "7"))

	provisioning := at(t, root, "Resources", "PennsieveComputeRole", "Properties")
	assert.Equal(t, "RoleName", at(t, provisioning, "RoleName").Value)
	assert.Equal(t, "!Ref", at(t, provisioning, "RoleName").Tag)
	assert.Equal(t, "7200", at(t, provisioning, "MaxSessionDuration").Value)

	runtime := at(t, root, "Resources", "PennsieveComputeRuntimeRole", "Properties")
	assert.Equal(t, "!Sub", at(t, runtime, "RoleName").Tag)
	assert.Equal(t, "${RoleName}"+policies.RuntimeRoleSuffix, at(t, runtime, "RoleName").Value)
	assert.Equal(t, "3600", at(t, runtime, "MaxSessionDuration").Value)

	for _, props := range []*yaml.Node{provisioning, runtime} {
		trust := at(t, props, "AssumeRolePolicyDocument", "Statement", 0)
		assert.Equal(t, "Allow", at(t, trust, "Effect").Value)
		assert.Equal(t, "sts:AssumeRole", at(t, trust, "Action").Value)
		assert.Equal(t, "arn:aws:iam::${PennsieveAccountId}:root", at(t, trust, "Principal", "AWS").Value)
		condition := at(t, trust, "Condition", "StringEquals", "sts:ExternalId")
		assert.Equal(t, "!Ref", condition.Tag)
		assert.Equal(t, "ExternalId", condition.Value)
		assert.Equal(t, "7", at(t, props, "Tags", 0, "Value").Value)
	}

	// The same three parameters as v1: the quick-create URL and the
	// CloudShell command (internal/handler/compute) don't change.
	var params map[string]any
	require.NoError(t, at(t, root, "Parameters").Decode(&params))
	assert.ElementsMatch(t, []string{"RoleName", "ExternalId", "PennsieveAccountId"}, keys(params))
	// A role name leaves room for "-runtime" within IAM's 64 characters.
	assert.Contains(t, at(t, root, "Parameters", "RoleName", "AllowedPattern").Value, "{1,56}")

	for _, o := range []string{"RoleArn", "RuntimeRoleArn", "RuntimeRoleName", "AccountId"} {
		at(t, root, "Outputs", o)
	}
	assert.Equal(t, "PennsieveComputeRuntimeRole.Arn", at(t, root, "Outputs", "RuntimeRoleArn", "Value").Value)

	assert.Equal(t, "7", at(t, root, "Metadata", "Pennsieve", "TemplateVersion").Value)
	assert.Contains(t, at(t, root, "Description").Value, "v7")
}

// Every placeholder is in the template once, and filled when rendered.
func TestTemplatePlaceholders(t *testing.T) {
	tpl, err := os.ReadFile("pennsieve-compute-role.yaml")
	require.NoError(t, err)
	for placeholder := range placeholders() {
		assert.Equal(t, 1, strings.Count(string(tpl), placeholder), placeholder)
	}
	assert.NotContains(t, render(t, "1"), "__")
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
