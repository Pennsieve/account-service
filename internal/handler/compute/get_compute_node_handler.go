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
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/service"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
)

// GetComputeNodeHandler retrieves a single compute node by ID
// GET /compute-nodes/{id}
//
// Required Permissions:
// - Must have access to the node (owner, shared user, workspace member, or team member)
// - If organization_id is provided, user must be a member of that organization
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

	// Get organization ID from query parameters (optional - empty means INDEPENDENT node)
	organizationId := request.QueryStringParameters["organization_id"]

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

	// If organization_id parameter is provided, validate it
	if organizationId != "" {
		// If the node is INDEPENDENT, organization_id parameter is not allowed
		if computeNode.OrganizationId == "INDEPENDENT" {
			log.Printf("Cannot access INDEPENDENT node %s with organization_id %s", uuid, organizationId)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusBadRequest,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrBadRequest),
			}, nil
		}
		
		// Validate that provided organization_id matches the compute node's existing organization
		if computeNode.OrganizationId != organizationId {
			log.Printf("Provided organization_id %s does not match compute node's organization %s", organizationId, computeNode.OrganizationId)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusForbidden,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrForbidden),
			}, nil
		}
		
		// Validate user is a member of the provided organization
		if validationResponse := utils.ValidateOrganizationMembership(ctx, cfg, userId, organizationId, handlerName); validationResponse != nil {
			return *validationResponse, nil
		}
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

	// Check account status if node is not Pending and get account owner
	// If account is Paused, override node status to Paused
	nodeStatus := computeNode.Status
	var accountOwnerId string
	accountsTable := os.Getenv("ACCOUNTS_TABLE")
	if accountsTable != "" {
		accountStore := store_dynamodb.NewAccountDatabaseStore(dynamoDBClient, accountsTable)
		account, err := accountStore.GetById(ctx, computeNode.AccountUuid)
		if err != nil {
			log.Printf("Error: could not fetch account %s: %v", computeNode.AccountUuid, err)
			// accountOwnerId remains empty - this is an error condition
		} else {
			// Get the account owner's userId
			accountOwnerId = account.UserId
			
			// Override node status to Paused if account is Paused and node is not Pending
			if computeNode.Status != "Pending" && account.Status == "Paused" {
				nodeStatus = "Paused"
			}
		}
	} else {
		log.Printf("Error: ACCOUNTS_TABLE environment variable not set")
	}
	
	if accountOwnerId == "" {
		log.Printf("Error: could not determine account owner for node %s (account: %s)", computeNode.Uuid, computeNode.AccountUuid)
	}

	// Get access scope for the node
	var accessScope models.NodeAccessScope
	permissions, err := permissionService.GetNodePermissions(ctx, computeNode.Uuid)
	if err != nil {
		log.Printf("Error getting node permissions: %v", err)
	} else {
		accessScope = permissions.AccessScope
	}

	// Convert INDEPENDENT back to empty string for API response consistency
	responseOrganizationId := computeNode.OrganizationId
	if computeNode.OrganizationId == "INDEPENDENT" {
		responseOrganizationId = ""
	}

	m, err := json.Marshal(models.Node{
		Uuid:        computeNode.Uuid,
		Name:        computeNode.Name,
		Description: computeNode.Description,
		QueueUrl:    computeNode.QueueUrl,
		Account: models.NodeAccount{
			Uuid:        computeNode.AccountUuid,
			AccountId:   computeNode.AccountId,
			AccountType: computeNode.AccountType,
			OwnerId:     accountOwnerId,
		},
		CreatedAt:          computeNode.CreatedAt,
		OrganizationId:     responseOrganizationId,
		OwnerId:            computeNode.UserId,
		Identifier:         computeNode.Identifier,
		WorkflowManagerTag: computeNode.WorkflowManagerTag,
		DeploymentMode:     computeNode.DeploymentMode,
		AccessScope:        accessScope,
		Status:             nodeStatus,
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