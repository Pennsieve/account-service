package compute

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/pennsieve/account-service/internal/clients"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
	"github.com/pennsieve/account-service/policies"
)

// rolePolicyDocument is the scoped IAM permission policy of a user's compute
// role: served to agents (GET /role-policy) and rendered into the
// CloudFormation template, both from policies/compute-role-policy.json, so the
// two can't drift. Change that file to change the policy for all future roles.
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
var rolePolicyDocument = policies.ComputeRolePolicy

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
	// PennsieveAccountId is the account the role trusts, with an external ID:
	// the template's PennsieveAccountId parameter.
	PennsieveAccountId string `json:"pennsieveAccountId,omitempty"`
	// CloudFormation is how to create the role from the published template,
	// with an external ID and when a template is published.
	CloudFormation *cloudFormationSetup `json:"cloudFormation,omitempty"`
}

// cloudFormationSetup is the compute role's template and two ways to create
// a stack from it with this role setup's parameters: the AWS console's
// quick-create page, or a command for AWS CloudShell (or any aws CLI).
type cloudFormationSetup struct {
	TemplateUrl       string `json:"templateUrl"`
	TemplateVersion   string `json:"templateVersion"`
	QuickCreateUrl    string `json:"quickCreateUrl"`
	CloudShellCommand string `json:"cloudShellCommand"`
}

// newCloudFormationSetup is the template at templateURL (version) with
// resp's parameters; nil without a template URL or an external ID. The stack
// is named after the role, in region.
func newCloudFormationSetup(templateURL, version, region string, resp roleConfigResponse) *cloudFormationSetup {
	if templateURL == "" || resp.ExternalId == "" {
		return nil
	}
	params := url.Values{}
	params.Set("templateURL", templateURL)
	params.Set("stackName", resp.RoleName)
	params.Set("param_RoleName", resp.RoleName)
	params.Set("param_ExternalId", resp.ExternalId)
	params.Set("param_PennsieveAccountId", resp.PennsieveAccountId)
	quickCreate := fmt.Sprintf("https://console.aws.amazon.com/cloudformation/home?region=%s#/stacks/quickcreate?%s",
		url.QueryEscape(region), quickCreateQuery(params))
	command := strings.Join([]string{
		"aws cloudformation create-stack",
		"--region " + region,
		"--stack-name " + resp.RoleName,
		"--template-url '" + templateURL + "'",
		"--parameters",
		"ParameterKey=RoleName,ParameterValue=" + resp.RoleName,
		"ParameterKey=ExternalId,ParameterValue=" + resp.ExternalId,
		"ParameterKey=PennsieveAccountId,ParameterValue=" + resp.PennsieveAccountId,
		"--capabilities CAPABILITY_NAMED_IAM",
	}, " ")
	return &cloudFormationSetup{
		TemplateUrl:       templateURL,
		TemplateVersion:   version,
		QuickCreateUrl:    quickCreate,
		CloudShellCommand: command,
	}
}

// quickCreateQuery is params in the console's order (templateURL, stackName,
// then the parameters), query-encoded.
func quickCreateQuery(params url.Values) string {
	order := []string{"templateURL", "stackName", "param_RoleName", "param_ExternalId", "param_PennsieveAccountId"}
	parts := make([]string, 0, len(order))
	for _, k := range order {
		parts = append(parts, k+"="+url.QueryEscape(params.Get(k)))
	}
	return strings.Join(parts, "&")
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
	resp := roleConfigResponse{
		RoleName:            roleName,
		PolicyDocument:      json.RawMessage(rolePolicyDocument),
		TrustPolicyDocument: json.RawMessage(trust),
		ExternalId:          externalId,
	}
	if externalId != "" {
		resp.PennsieveAccountId = pennsieveAccountId
	}
	return resp, nil
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

	region := os.Getenv("REGION")
	if region == "" {
		region = "us-east-1"
	}
	resp.CloudFormation = newCloudFormationSetup(os.Getenv("CFN_TEMPLATE_URL"), os.Getenv("CFN_TEMPLATE_VERSION"), region, resp)

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
