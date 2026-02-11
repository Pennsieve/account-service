package compute

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
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
	
	// Get organization ID from query parameters (optional - empty means INDEPENDENT node)
	organizationId := request.QueryStringParameters["organization_id"]
	
	// Load AWS config
	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}
	
	// Initialize container to get PostgreSQL connection
	appContainer, err := utils.GetContainer(ctx, cfg)
	if err != nil {
		log.Printf("Error getting container: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	db := appContainer.PostgresDB()
	if db == nil {
		log.Printf("PostgreSQL connection required but unavailable for permission operations (handler=%s, nodeId=%s)", 
			handlerName, nodeUuid)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}
	
	// Initialize stores
	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	nodeAccessTable := os.Getenv("NODE_ACCESS_TABLE")
	nodesTable := os.Getenv("COMPUTE_NODES_TABLE")
	nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(dynamoDBClient, nodeAccessTable)
	nodeStore := store_dynamodb.NewNodeDatabaseStore(dynamoDBClient, nodesTable)
	
	// Get the node to validate organization context
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
	
	// If organization_id parameter is provided, validate it
	if organizationId != "" {
		// Validate that provided organization_id matches the compute node's existing organization
		if node.OrganizationId != organizationId && node.OrganizationId != "INDEPENDENT" {
			log.Printf("Provided organization_id %s does not match compute node's organization %s", organizationId, node.OrganizationId)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusBadRequest,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrBadRequest),
			}, nil
		}
		
		// Validate user is a member of the provided organization
		if validationResponse := utils.ValidateOrganizationMembership(ctx, cfg, userId, organizationId, handlerName); validationResponse != nil {
			return *validationResponse, nil
		}
	}
	
	teamStore := store_postgres.NewPostgresTeamStore(db)
	orgStore := store_postgres.NewPostgresOrganizationStore(db)
	
	permissionService := service.NewPermissionService(nodeAccessStore, teamStore)
	permissionService.SetNodeStore(nodeStore)
	permissionService.SetOrganizationStore(orgStore)
	
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