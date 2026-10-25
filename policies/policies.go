// Package policies holds the IAM documents of a user's compute role, as the
// single source for both GET /role-policy and the CloudFormation template
// (cloudformation/pennsieve-compute-role.yaml, rendered by terraform).
package policies

import _ "embed"

// ComputeRolePolicy is the compute role's inline permission policy.
//
//go:embed compute-role-policy.json
var ComputeRolePolicy string
