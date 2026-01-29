package compute

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/account-service/internal/utils"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/service"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

// GetComputeNodeHandler retrieves a single compute node by ID
// GET /compute-nodes/{id}
//
// Required Permissions:
// - Must have access to the node (owner, shared user, workspace member, or team member)
func GetComputeNodeHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetComputeNodeHandler"
	uuid := request.PathParameters["id"]
	
	// Validate that ID is provided
	if uuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrBadRequest),
		}, nil
	}

	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}
	// Get user information from request
	userId, err := utils.GetUserIdFromRequest(request)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	// Get organization ID from claims if present
	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	organizationId := claims.OrgClaim.NodeId

	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	computeNodesTable := os.Getenv("COMPUTE_NODES_TABLE")

	dynamo_store := store_dynamodb.NewNodeDatabaseStore(dynamoDBClient, computeNodesTable)
	computeNode, err := dynamo_store.GetById(ctx, uuid)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}
	if (models.DynamoDBNode{}) == computeNode {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrNoRecordsFound),
		}, nil
	}

	// Validate node organization matches user's organization context
	// Skip validation for INDEPENDENT nodes (organization-independent nodes)
	if organizationId != "" && computeNode.OrganizationId != "INDEPENDENT" && computeNode.OrganizationId != organizationId {
		log.Printf("Node %s belongs to organization '%s' but user claims organization '%s'", 
			uuid, computeNode.OrganizationId, organizationId)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrForbidden),
		}, nil
	}

	// Check if user has access to the node
	nodeAccessTable := os.Getenv("NODE_ACCESS_TABLE")
	if nodeAccessTable == "" {
		log.Println("NODE_ACCESS_TABLE environment variable not set")
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(dynamoDBClient, nodeAccessTable)
	permissionService := service.NewPermissionService(nodeAccessStore, nil)

	// Check if the user has access to this node
	hasAccess, err := permissionService.CheckNodeAccess(ctx, userId, computeNode.Uuid, organizationId)
	if err != nil {
		log.Printf("Error checking node access: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	if !hasAccess {
		log.Printf("User %s does not have access to node %s", userId, uuid)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrForbidden),
		}, nil
	}

	// Convert INDEPENDENT back to empty string for API response consistency
	responseOrganizationId := computeNode.OrganizationId
	if computeNode.OrganizationId == "INDEPENDENT" {
		responseOrganizationId = ""
	}

	m, err := json.Marshal(models.Node{
		Uuid:                  computeNode.Uuid,
		Name:                  computeNode.Name,
		Description:           computeNode.Description,
		ComputeNodeGatewayUrl: computeNode.ComputeNodeGatewayUrl,
		EfsId:                 computeNode.EfsId,
		QueueUrl:              computeNode.QueueUrl,
		Account: models.NodeAccount{
			Uuid:        computeNode.AccountUuid,
			AccountId:   computeNode.AccountId,
			AccountType: computeNode.AccountType,
		},
		CreatedAt:          computeNode.CreatedAt,
		OrganizationId:     responseOrganizationId,
		UserId:             computeNode.UserId,
		Identifier:         computeNode.Identifier,
		WorkflowManagerTag: computeNode.WorkflowManagerTag,
		Status:             computeNode.Status,
	})
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMarshaling),
		}, nil
	}
	response := events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(m),
	}
	return response, nil
}