package compute

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/pennsieve/account-service/internal/clients"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
)

// rolePolicyDocument is the scoped IAM permission policy served to agents
// during role creation. Update this document to change the policy for all
// future roles.
//
// The policy uses permissions boundaries to prevent privilege escalation:
// - Statement 1: Allow service actions (no IAM mutation, plus iam:CreateServiceLinkedRole)
// - Statement 2: Allow IAM role/policy management for Terraform
// - Statement 3: Allow read-only CloudWatch metric reads (no writes, no alarms, no logs)
// - Statement 4: Deny CreateRole without permissions boundary attached
// - Statement 5: Deny removing permissions boundaries from roles
// - Statement 6: Deny modifying the cross-account role itself (Pennsieve-Compute-* and legacy ROLE-*)
//
// AllowMetricReads deliberately carries no Condition. A
// `cloudwatch:namespace` condition reads like tighter scoping, but that key
// is only evaluated for publish actions (PutMetricData) — on ListMetrics,
// GetMetricData and GetMetricStatistics it is never satisfied, so adding it
// denies every read instead of narrowing it. Verified empirically: the same
// policy with the condition returns AccessDenied for AWS/S3, without it
// returns the data. CloudWatch metric reads support no resource-level
// scoping either, hence Resource "*".
const rolePolicyDocument = `{
	"Version": "2012-10-17",
	"Statement": [
		{
			"Sid": "AllowServices",
			"Effect": "Allow",
			"Action": [
				"ec2:*",
				"ecr:*",
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
				"ssm:DescribeParameters",
				"ssm:AddTagsToResource",
				"ssm:RemoveTagsFromResource",
				"ssm:ListTagsForResource",
				"autoscaling:*",
				"elasticloadbalancing:*",
				"route53:*",
				"acm:*",
				"events:PutRule",
				"events:DeleteRule",
				"events:DescribeRule",
				"events:EnableRule",
				"events:DisableRule",
				"events:PutTargets",
				"events:RemoveTargets",
				"events:ListTargetsByRule",
				"events:TagResource",
				"events:UntagResource",
				"events:ListTagsForResource",
				"bedrock:CreateGuardrail",
				"bedrock:DeleteGuardrail",
				"bedrock:GetGuardrail",
				"bedrock:UpdateGuardrail",
				"bedrock:CreateGuardrailVersion",
				"bedrock:DeleteGuardrailVersion",
				"bedrock:ListGuardrails",
				"bedrock:TagResource",
				"bedrock:UntagResource",
				"bedrock:ListTagsForResource",
				"sts:GetCallerIdentity",
				"iam:CreateServiceLinkedRole"
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
			"Sid": "AllowMetricReads",
			"Effect": "Allow",
			"Action": [
				"cloudwatch:ListMetrics",
				"cloudwatch:GetMetricData",
				"cloudwatch:GetMetricStatistics"
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

// RolePolicyJSON returns the raw role policy document for testing.
func RolePolicyJSON() string {
	return rolePolicyDocument
}

// roleConfigResponse is the JSON envelope returned by the role-policy endpoint.
type roleConfigResponse struct {
	RoleName            string          `json:"roleName"`
	PolicyDocument      json.RawMessage `json:"policyDocument"`
	TrustPolicyDocument json.RawMessage `json:"trustPolicyDocument"`
	// ExternalId is the sts:ExternalId the trust policy requires; the agent
	// registers the account with it. Only for agents that ask (?externalId=true),
	// so older agents get exactly the configuration they always did.
	ExternalId string `json:"externalId,omitempty"`
}

// buildRoleConfig is the role configuration for a new role trusting
// pennsieveAccountId, with a freshly issued external ID when withExternalId.
func buildRoleConfig(roleName, pennsieveAccountId string, withExternalId bool, newExternalId func() (string, error)) (roleConfigResponse, error) {
	externalId := ""
	if withExternalId {
		id, err := newExternalId()
		if err != nil {
			return roleConfigResponse{}, err
		}
		externalId = id
	}
	trust, err := clients.TrustPolicy(pennsieveAccountId, externalId)
	if err != nil {
		return roleConfigResponse{}, err
	}
	return roleConfigResponse{
		RoleName:            roleName,
		PolicyDocument:      json.RawMessage(rolePolicyDocument),
		TrustPolicyDocument: json.RawMessage(trust),
		ExternalId:          externalId,
	}, nil
}

// issueRoleCode records the external ID in resp as issued to userId, for
// resp's role, until ExternalIdLifetime from now.
func issueRoleCode(ctx context.Context, codes store_dynamodb.RoleCodeStore, resp roleConfigResponse, userId string, now time.Time) error {
	return codes.Issue(ctx, store_dynamodb.RoleCode{
		ExternalId: resp.ExternalId,
		UserId:     userId,
		RoleName:   resp.RoleName,
		IssuedAt:   now.Unix(),
		ExpiresAt:  now.Add(clients.ExternalIdLifetime).Unix(),
	})
}

// GetRolePolicyHandler returns the role configuration (name + permission policy +
// trust policy) that agents use when creating cross-account roles.
// GET /role-policy
func GetRolePolicyHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	env := strings.ToLower(os.Getenv("ENV"))
	if env == "" {
		env = "dev"
	}
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       `{"error":"failed to generate role name"}`,
		}, nil
	}
	roleName := fmt.Sprintf("Pennsieve-Compute-%s-%s", env, hex.EncodeToString(suffix))

	// Get the Pennsieve provisioner account ID to build the trust policy
	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       `{"error":"failed to load AWS config"}`,
		}, nil
	}
	stsClient := sts.NewFromConfig(cfg)
	identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       `{"error":"failed to get provisioner account ID"}`,
		}, nil
	}

	withExternalId := request.QueryStringParameters["externalId"] == "true"
	resp, err := buildRoleConfig(roleName, *identity.Account, withExternalId, clients.NewRandomExternalId)
	if err != nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       `{"error":"failed to build role config"}`,
		}, nil
	}

	// Pennsieve remembers whom it gave the code to, for which role: only that
	// user can register that role with it (POST /accounts).
	if withExternalId {
		userId, err := utils.GetUserIdFromRequest(request)
		if err != nil {
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusUnauthorized,
				Body:       `{"error":"no user on the request"}`,
			}, nil
		}
		codes := store_dynamodb.NewRoleCodeStore(dynamodb.NewFromConfig(cfg), os.Getenv("ROLE_CODES_TABLE"))
		if err := issueRoleCode(ctx, codes, resp, userId, time.Now()); err != nil {
			log.Printf("role-policy: %v", err)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       `{"error":"failed to issue the role's external id"}`,
			}, nil
		}
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
