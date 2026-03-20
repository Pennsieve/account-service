package authorizer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

type DirectAuthorizeRequest struct {
	UserNodeID         string `json:"user_node_id"`
	OrganizationNodeID string `json:"organization_node_id,omitempty"`
	DatasetNodeID      string `json:"dataset_node_id,omitempty"`
}

type DirectAuthorizeResponse struct {
	IsAuthorized bool                   `json:"is_authorized"`
	Claims       map[string]interface{} `json:"claims,omitempty"`
	Error        string                 `json:"error,omitempty"`
}

// DirectAuthorizer is an interface for invoking the direct authorizer.
// This allows mocking in tests.
type DirectAuthorizer interface {
	Authorize(ctx context.Context, userNodeId, organizationId string) (*DirectAuthorizeResponse, error)
}

// LambdaDirectAuthorizer implements DirectAuthorizer using an AWS Lambda client.
type LambdaDirectAuthorizer struct {
	Client *lambda.Client
}

func NewLambdaDirectAuthorizer(client *lambda.Client) *LambdaDirectAuthorizer {
	return &LambdaDirectAuthorizer{Client: client}
}

func (a *LambdaDirectAuthorizer) Authorize(ctx context.Context, userNodeId, organizationId string) (*DirectAuthorizeResponse, error) {
	return InvokeDirectAuthorizer(ctx, a.Client, userNodeId, organizationId)
}

// VerifyOrganizationAccess invokes the direct authorizer Lambda to check whether
// the given user is a member of the given organization.
func VerifyOrganizationAccess(ctx context.Context, lambdaClient *lambda.Client, userNodeId, organizationId string) (bool, error) {
	resp, err := InvokeDirectAuthorizer(ctx, lambdaClient, userNodeId, organizationId)
	if err != nil {
		return false, err
	}
	return resp.IsAuthorized, nil
}

// InvokeDirectAuthorizer calls the direct authorizer Lambda and returns the full
// response including claims (user, org, team memberships).
func InvokeDirectAuthorizer(ctx context.Context, lambdaClient *lambda.Client, userNodeId, organizationId string) (*DirectAuthorizeResponse, error) {
	directAuthorizerName := os.Getenv("DIRECT_AUTHORIZER_LAMBDA_NAME")
	if directAuthorizerName == "" {
		return nil, fmt.Errorf("DIRECT_AUTHORIZER_LAMBDA_NAME not set")
	}

	authReq := DirectAuthorizeRequest{
		UserNodeID:         userNodeId,
		OrganizationNodeID: organizationId,
	}
	payload, err := json.Marshal(authReq)
	if err != nil {
		return nil, fmt.Errorf("marshaling auth request: %w", err)
	}

	result, err := lambdaClient.Invoke(ctx, &lambda.InvokeInput{
		FunctionName:   aws.String(directAuthorizerName),
		InvocationType: lambdatypes.InvocationTypeRequestResponse,
		Payload:        payload,
	})
	if err != nil {
		return nil, fmt.Errorf("invoking direct authorizer: %w", err)
	}

	var authResp DirectAuthorizeResponse
	if err := json.Unmarshal(result.Payload, &authResp); err != nil {
		return nil, fmt.Errorf("unmarshaling auth response: %w", err)
	}

	return &authResp, nil
}

// IsOrgAdmin checks if the user has admin permissions (Role >= 16) based on
// the org_claim in the direct authorizer response claims.
func IsOrgAdmin(claims map[string]interface{}) bool {
	orgClaimRaw, ok := claims["org_claim"]
	if !ok || orgClaimRaw == nil {
		return false
	}

	orgClaim, ok := orgClaimRaw.(map[string]interface{})
	if !ok {
		return false
	}

	role, ok := orgClaim["Role"].(float64)
	if !ok {
		return false
	}

	return int(role) >= 16
}

// ExtractTeamNodeIds extracts team node IDs from direct authorizer claims.
func ExtractTeamNodeIds(claims map[string]interface{}) []string {
	teamClaimsRaw, ok := claims["team_claims"]
	if !ok {
		return nil
	}

	teamClaimsArr, ok := teamClaimsRaw.([]interface{})
	if !ok {
		return nil
	}

	var nodeIds []string
	for _, tc := range teamClaimsArr {
		teamMap, ok := tc.(map[string]interface{})
		if !ok {
			continue
		}
		if nodeId, ok := teamMap["NodeId"].(string); ok && nodeId != "" {
			nodeIds = append(nodeIds, nodeId)
		}
	}
	return nodeIds
}
