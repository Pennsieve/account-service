package compute

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
)

// rolePolicyDocument is the scoped IAM permission policy served to agents
// during role creation. Update this document to change the policy for all
// future roles.
//
// The policy uses permissions boundaries to prevent privilege escalation:
// - Statement 1: Allow service actions (no IAM mutation)
// - Statement 2: Allow IAM role/policy management for Terraform
// - Statement 3: Deny CreateRole without permissions boundary attached
// - Statement 4: Deny removing permissions boundaries from roles
// - Statement 5: Deny modifying the cross-account role itself (Pennsieve-Compute-* and legacy ROLE-*)
const rolePolicyDocument = `{
	"Version": "2012-10-17",
	"Statement": [
		{
			"Sid": "AllowServices",
			"Effect": "Allow",
			"Action": [
				"ec2:*",
				"ecs:*",
				"elasticfilesystem:*",
				"lambda:*",
				"s3:*",
				"states:*",
				"kms:*",
				"logs:*",
				"dynamodb:*",
				"ssm:GetParameter",
				"ssm:PutParameter",
				"ssm:DeleteParameter",
				"ssm:GetParameters",
				"ssm:AddTagsToResource",
				"ssm:RemoveTagsFromResource",
				"ssm:ListTagsForResource",
				"autoscaling:*",
				"sts:GetCallerIdentity"
			],
			"Resource": "*"
		},
		{
			"Sid": "AllowIAMRoleManagement",
			"Effect": "Allow",
			"Action": [
				"iam:CreateRole",
				"iam:DeleteRole",
				"iam:GetRole",
				"iam:PassRole",
				"iam:TagRole",
				"iam:UntagRole",
				"iam:PutRolePolicy",
				"iam:GetRolePolicy",
				"iam:DeleteRolePolicy",
				"iam:AttachRolePolicy",
				"iam:DetachRolePolicy",
				"iam:ListRolePolicies",
				"iam:ListAttachedRolePolicies",
				"iam:CreateInstanceProfile",
				"iam:DeleteInstanceProfile",
				"iam:AddRoleToInstanceProfile",
				"iam:RemoveRoleFromInstanceProfile",
				"iam:GetInstanceProfile",
				"iam:ListInstanceProfilesForRole",
				"iam:CreatePolicy",
				"iam:DeletePolicy",
				"iam:GetPolicy",
				"iam:GetPolicyVersion",
				"iam:ListPolicyVersions",
				"iam:CreatePolicyVersion",
				"iam:DeletePolicyVersion"
			],
			"Resource": "*"
		},
		{
			"Sid": "DenyCreateRoleWithoutBoundary",
			"Effect": "Deny",
			"Action": "iam:CreateRole",
			"Resource": "*",
			"Condition": {
				"StringNotLike": {
					"iam:PermissionsBoundary": "arn:aws:iam::*:policy/pennsieve-boundary-*"
				}
			}
		},
		{
			"Sid": "DenyBoundaryStripping",
			"Effect": "Deny",
			"Action": "iam:DeleteRolePermissionsBoundary",
			"Resource": "*"
		},
		{
			"Sid": "DenySelfModification",
			"Effect": "Deny",
			"Action": [
				"iam:PutRolePolicy",
				"iam:DeleteRolePolicy",
				"iam:AttachRolePolicy",
				"iam:DetachRolePolicy",
				"iam:UpdateRole",
				"iam:UpdateAssumeRolePolicy"
			],
			"Resource": [
				"arn:aws:iam::*:role/Pennsieve-Compute-*",
				"arn:aws:iam::*:role/ROLE-*"
			]
		}
	]
}`

// roleConfigResponse is the JSON envelope returned by the role-policy endpoint.
type roleConfigResponse struct {
	RoleName       string          `json:"roleName"`
	PolicyDocument json.RawMessage `json:"policyDocument"`
}

// GetRolePolicyHandler returns the role configuration (name + permission policy)
// that agents use when creating cross-account roles.
// GET /role-policy
func GetRolePolicyHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	env := strings.ToLower(os.Getenv("ENV"))
	if env == "" {
		env = "dev"
	}
	roleName := fmt.Sprintf("Pennsieve-Compute-%s", env)

	resp := roleConfigResponse{
		RoleName:       roleName,
		PolicyDocument: json.RawMessage(rolePolicyDocument),
	}

	body, err := json.Marshal(resp)
	if err != nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       `{"error":"failed to marshal role config"}`,
		}, nil
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(body),
	}, nil
}