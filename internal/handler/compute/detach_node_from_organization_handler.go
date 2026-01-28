package compute

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/service"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
	"github.com/pennsieve/account-service/internal/errors"
)

func DetachNodeFromOrganizationHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "DetachNodeFromOrganizationHandler"
	
	nodeId := request.PathParameters["id"]
	if nodeId == "" {
		log.Println("Node ID is required")
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMissingNodeUuid),
		}, nil
	}

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	userId := claims.UserClaim.NodeId

	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Printf("Error loading AWS config: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	
	// Get the node to check which account it belongs to
	nodesTable := os.Getenv("COMPUTE_NODES_TABLE")
	nodeStore := store_dynamodb.NewNodeDatabaseStore(dynamoDBClient, nodesTable)
	node, err := nodeStore.GetById(ctx, nodeId)
	if err != nil {
		log.Printf("Error getting node: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrNotFound),
		}, nil
	}
	
	// Only the account owner can detach nodes (regardless of who created them)
	if node.AccountUuid != "" {
		accountsTable := os.Getenv("ACCOUNTS_TABLE")
		accountStore := store_dynamodb.NewAccountDatabaseStore(dynamoDBClient, accountsTable)
		account, err := accountStore.GetById(ctx, node.AccountUuid)
		if err != nil {
			log.Printf("Error getting account: %v", err)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
			}, nil
		}
		
		// Only the account owner can detach nodes
		if account.UserId != userId {
			log.Printf("User %s is not the account owner (%s), cannot detach node", userId, account.UserId)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusForbidden,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrOnlyAccountOwnerCanDetachNodes),
			}, nil
		}
	}
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

	err = permissionService.DetachNodeFromOrganization(ctx, nodeId, userId)
	if err != nil {
		log.Printf("Error detaching node from organization: %v", err)
		
		switch err {
		case models.ErrOrganizationIndependentNodeCannotBeShared:
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusBadRequest,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrOrganizationIndependentNodeCannotBeShared),
			}, nil
		case errors.ErrForbidden:
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusForbidden,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrForbidden),
			}, nil
		default:
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
			}, nil
		}
	}

	response := struct {
		Message    string `json:"message"`
		NodeUuid   string `json:"nodeUuid"`
		Action     string `json:"action"`
		EntityType string `json:"entityType"`
		EntityId   string `json:"entityId"`
	}{
		Message:    "Successfully detached node from organization",
		NodeUuid:   nodeId,
		Action:     "detached_from_organization",
		EntityType: "organization",
		EntityId:   "", // No specific organization since it was detached
	}

	responseBody, err := json.Marshal(response)
	if err != nil {
		log.Printf("Error marshaling response: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMarshaling),
		}, nil
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(responseBody),
	}, nil
}