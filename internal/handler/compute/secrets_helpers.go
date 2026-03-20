package compute

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	authclient "github.com/pennsieve/account-service/internal/authorizer"
	"github.com/pennsieve/account-service/internal/clients"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/service"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

var validKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// reservedSecretKeys are system-managed names that user secrets must not shadow.
var reservedSecretKeys = map[string]bool{
	// ECS environment variable names
	"EXECUTION_RUN_ID": true, "WORKFLOW_INSTANCE_ID": true, "INTEGRATION_ID": true,
	"INPUT_DIR": true, "OUTPUT_DIR": true,
	"SESSION_TOKEN": true, "REFRESH_TOKEN": true,
	"PENNSIEVE_API_HOST": true, "PENNSIEVE_API_HOST2": true,
	"DEPLOYMENT_MODE": true, "LLM_GOVERNOR_FUNCTION": true,
	"DATASET_ID": true, "ORGANIZATION_ID": true, "TARGET_TYPE": true,
	"CALLBACK_TOKEN": true,
	// Lambda payload keys
	"executionRunId": true, "integrationId": true, "computeNodeId": true,
	"inputDir": true, "outputDir": true,
	"sessionToken": true, "refreshToken": true,
	"datasetId": true, "organizationId": true, "targetType": true,
	"callbackToken": true, "llmGovernorFunction": true, "params": true,
}

func validateSecretKeys(secrets map[string]string) error {
	for k := range secrets {
		if !validKeyPattern.MatchString(k) {
			return fmt.Errorf("invalid secret key %q: must match [A-Za-z_][A-Za-z0-9_]*", k)
		}
		if reservedSecretKeys[k] {
			return fmt.Errorf("reserved secret key %q: this name is used by the system", k)
		}
	}
	return nil
}

// secretsContext holds the resolved dependencies for secrets handlers.
type secretsContext struct {
	UserID            string
	NodeUuid          string
	Node              models.DynamoDBNode
	ProvisionerClient *clients.ProvisionerClient
}

// initSecretsContext performs the common setup for all secrets handlers:
// extracts user ID, node UUID, checks access, fetches the node, and builds the provisioner client.
func initSecretsContext(ctx context.Context, request events.APIGatewayV2HTTPRequest, handlerName string, requireOwner bool) (*secretsContext, *events.APIGatewayV2HTTPResponse) {
	nodeUuid := request.PathParameters["id"]
	if nodeUuid == "" {
		resp := events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMissingNodeUuid),
		}
		return nil, &resp
	}

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	userId := claims.UserClaim.NodeId

	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		resp := events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}
		return nil, &resp
	}

	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	nodesTable := os.Getenv("COMPUTE_NODES_TABLE")
	nodeStore := store_dynamodb.NewNodeDatabaseStore(dynamoDBClient, nodesTable)

	node, err := nodeStore.GetById(ctx, nodeUuid)
	if err != nil {
		log.Printf("Error getting node %s: %v", nodeUuid, err)
		resp := events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}
		return nil, &resp
	}
	if node.Uuid == "" {
		resp := events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrNotFound),
		}
		return nil, &resp
	}

	if requireOwner {
		canManage := node.UserId == userId
		if !canManage && node.OrganizationId != "" && node.OrganizationId != "INDEPENDENT" {
			lambdaClient := lambda.NewFromConfig(cfg)
			nodeAccessTable := os.Getenv("NODE_ACCESS_TABLE")
			accountWorkspaceTable := os.Getenv("ACCOUNT_WORKSPACE_TABLE")
			nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(dynamoDBClient, nodeAccessTable)
			permissionService := service.NewPermissionService(nodeAccessStore, nil)
			permissionService.SetAuthorizer(authclient.NewLambdaDirectAuthorizer(lambdaClient))
			permissionService.SetAccountWorkspaceStore(store_dynamodb.NewAccountWorkspaceStore(dynamoDBClient, accountWorkspaceTable))
			isAdmin, err := permissionService.IsAdminWithManageAccess(ctx, userId, node.OrganizationId, node.AccountUuid)
			if err == nil && isAdmin {
				canManage = true
			}
		}
		if !canManage {
			resp := events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusForbidden,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrOnlyOwnerCanChangePermissions),
			}
			return nil, &resp
		}
	} else {
		lambdaClient := lambda.NewFromConfig(cfg)
		if errResp := checkNodeAccess(ctx, lambdaClient, dynamoDBClient, handlerName, userId, nodeUuid, node.OrganizationId); errResp != nil {
			return nil, errResp
		}
	}

	if node.ComputeNodeGatewayUrl == "" {
		resp := events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrBadRequest),
		}
		return nil, &resp
	}

	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}

	pc := clients.NewProvisionerClient(node.ComputeNodeGatewayUrl, region, cfg)

	return &secretsContext{
		UserID:            userId,
		NodeUuid:          nodeUuid,
		Node:              node,
		ProvisionerClient: pc,
	}, nil
}

func checkNodeAccess(ctx context.Context, lambdaClient *lambda.Client, dynamoDBClient *dynamodb.Client, handlerName, userId, nodeUuid, organizationId string) *events.APIGatewayV2HTTPResponse {
	nodeAccessTable := os.Getenv("NODE_ACCESS_TABLE")
	nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(dynamoDBClient, nodeAccessTable)
	computeNodesTable := os.Getenv("COMPUTE_NODES_TABLE")

	accountWorkspaceTable := os.Getenv("ACCOUNT_WORKSPACE_TABLE")

	permissionService := service.NewPermissionService(nodeAccessStore, nil)
	permissionService.SetAuthorizer(authclient.NewLambdaDirectAuthorizer(lambdaClient))
	permissionService.SetNodeStore(store_dynamodb.NewNodeDatabaseStore(dynamoDBClient, computeNodesTable))
	permissionService.SetAccountWorkspaceStore(store_dynamodb.NewAccountWorkspaceStore(dynamoDBClient, accountWorkspaceTable))

	hasAccess, err := permissionService.CheckNodeAccess(ctx, userId, nodeUuid, organizationId)
	if err != nil {
		log.Printf("Error checking node access: %v", err)
		resp := events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrCheckingAccess),
		}
		return &resp
	}
	if !hasAccess {
		resp := events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrForbidden),
		}
		return &resp
	}

	return nil
}