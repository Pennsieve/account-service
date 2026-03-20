package compute

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	authclient "github.com/pennsieve/account-service/internal/authorizer"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/service"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/store_postgres"
	"github.com/pennsieve/account-service/internal/utils"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)


// GetNodePermissionsHandler retrieves the permissions for a compute node
// GET /compute-nodes/{id}/permissions
//
// Required Permissions:
// - Must have access to the node (owner, shared user, workspace member, or team member)
// - If organization_id is provided, user must be a member of that organization
func GetNodePermissionsHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetNodePermissionsHandler"
	
	// Get node UUID from path
	nodeUuid := request.PathParameters["id"]
	if nodeUuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMissingNodeUuid),
		}, nil
	}
	
	// Get user claims
	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	userId := claims.UserClaim.NodeId
	
	// Load AWS config
	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	// Initialize stores
	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	lambdaClient := lambda.NewFromConfig(cfg)
	nodeAccessTable := os.Getenv("NODE_ACCESS_TABLE")
	nodesTable := os.Getenv("COMPUTE_NODES_TABLE")
	nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(dynamoDBClient, nodeAccessTable)
	nodeStore := store_dynamodb.NewNodeDatabaseStore(dynamoDBClient, nodesTable)

	// Get the node
	node, err := nodeStore.GetById(ctx, nodeUuid)
	if err != nil {
		log.Printf("Error fetching node %s: %v", nodeUuid, err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}
	if node.Uuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrNoRecordsFound),
		}, nil
	}

	// Use the node's own organization for access checks
	organizationId := ""
	if node.OrganizationId != "" && node.OrganizationId != "INDEPENDENT" {
		organizationId = node.OrganizationId
	}
	
	permissionService := service.NewPermissionService(nodeAccessStore, nil)
	permissionService.SetNodeStore(nodeStore)
	permissionService.SetAuthorizer(authclient.NewLambdaDirectAuthorizer(lambdaClient))
	accountWorkspaceTable := os.Getenv("ACCOUNT_WORKSPACE_TABLE")
	permissionService.SetAccountWorkspaceStore(store_dynamodb.NewAccountWorkspaceStore(dynamoDBClient, accountWorkspaceTable))

	// Check if user has access to the node
	hasAccess, err := permissionService.CheckNodeAccess(ctx, userId, nodeUuid, organizationId)
	if err != nil {
		log.Printf("error checking access: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrCheckingAccess),
		}, nil
	}
	
	if !hasAccess {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrForbidden),
		}, nil
	}
	
	// Set up Postgres stores for GetNodePermissions (stale permission cleanup)
	appContainer, err := utils.GetContainer(ctx, cfg)
	if err == nil {
		db := appContainer.PostgresDB()
		if db != nil {
			permissionService.TeamStore = store_postgres.NewPostgresTeamStore(db)
			permissionService.SetOrganizationStore(store_postgres.NewPostgresOrganizationStore(db))
		}
	}

	// Get permissions
	permissions, err := permissionService.GetNodePermissions(ctx, nodeUuid)
	if err != nil {
		log.Printf("error getting permissions: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrGettingPermissions),
		}, nil
	}
	
	response, err := json.Marshal(permissions)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMarshaling),
		}, nil
	}
	
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(response),
	}, nil
}