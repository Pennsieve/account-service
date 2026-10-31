// Package policies holds the IAM documents of a user's compute roles, as the
// single source for both GET /role-policy and the CloudFormation template
// (cloudformation/pennsieve-compute-role.yaml, rendered by terraform).
//
// Template v2 makes two roles in the user's account, both trusting Pennsieve
// with the registration's external ID:
//
//   - the provisioning role (the account's roleName), used only by the
//     provisioner tasks that create, update and destroy nodes, and by the app
//     provisioner. Its permissions are three customer-managed policies, as
//     one would exceed IAM's 10,240-character limit;
//   - the runtime role (roleName + "-runtime"), used for everything else
//     Pennsieve does in the account day to day: calling a node's compute
//     gateway, finding a notebook session's task and routing to it, reading
//     a run's results, and reading storage metrics.
//
// Keep each provisioning document under 6,144 characters without whitespace
// (a managed policy's limit) and the runtime document under 10,240 (inline).
package policies

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

import _ "embed"

//go:embed compute-provisioning-iam-policy.json
var provisioningIam string

//go:embed compute-provisioning-infrastructure-policy.json
var provisioningInfrastructure string

//go:embed compute-provisioning-services-policy.json
var provisioningServices string

// RuntimeRolePolicy is the runtime role's inline permission policy.
//
//go:embed compute-runtime-role-policy.json
var RuntimeRolePolicy string

// Policy is one of the provisioning role's managed policies.
type Policy struct {
	// Name is the policy's suffix: the managed policy is named
	// "<roleName>-<Name>" in the user's account.
	Name string
	// Document is the policy document, as in the file.
	Document string
}

// ProvisioningPolicies are the provisioning role's managed policies, in the
// template's order.
var ProvisioningPolicies = []Policy{
	{Name: "iam", Document: provisioningIam},
	{Name: "infrastructure", Document: provisioningInfrastructure},
	{Name: "services", Document: provisioningServices},
}

// RuntimeRoleSuffix follows the provisioning role's name in the runtime
// role's: the template names it "${RoleName}-runtime".
const RuntimeRoleSuffix = "-runtime"

// RuntimeRoleName is the runtime role made with the provisioning role
// roleName.
func RuntimeRoleName(roleName string) string {
	return roleName + RuntimeRoleSuffix
}

// ComputeRolePolicy is the provisioning role's whole permission policy, the
// statements of ProvisioningPolicies in one document: what GET /role-policy
// serves as policyDocument. Too large for one inline policy; for reading.
var ComputeRolePolicy = merge(ProvisioningPolicies)

func merge(policies []Policy) string {
	type document struct {
		Version   string            `json:"Version"`
		Statement []json.RawMessage `json:"Statement"`
	}
	merged := document{Version: "2012-10-17"}
	for _, p := range policies {
		var d document
		if err := json.Unmarshal([]byte(p.Document), &d); err != nil {
			panic(fmt.Sprintf("policies: %s: %v", p.Name, err))
		}
		merged.Statement = append(merged.Statement, d.Statement...)
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetIndent("", "\t")
	if err := enc.Encode(merged); err != nil {
		panic(err)
	}
	return strings.TrimSpace(out.String()) + "\n"
}
