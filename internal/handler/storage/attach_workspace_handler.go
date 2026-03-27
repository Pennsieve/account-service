package storage

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

type AttachWorkspaceRequest struct {
	WorkspaceId string `json:"workspaceId"`
	IsDefault   bool   `json:"isDefault"`
}

// AttachToWorkspaceHandler attaches a storage node to a workspace
// POST /storage-nodes/{id}/workspace
func AttachToWorkspaceHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "AttachToWorkspaceHandler"
	nodeId := request.PathParameters["id"]

	if nodeId == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrBadRequest),
		}, nil
	}

	var req AttachWorkspaceRequest
	if err := json.Unmarshal([]byte(request.Body), &req); err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}

	if req.WorkspaceId == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMissingWorkspaceId),
		}, nil
	}

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	userId := claims.UserClaim.NodeId

	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	storageNodesTable := os.Getenv("STORAGE_NODES_TABLE")
	storageNodeStore := store_dynamodb.NewStorageNodeDatabaseStore(dynamoDBClient, storageNodesTable)

	node, err := storageNodeStore.GetById(ctx, nodeId)
	if err != nil {
		log.Printf("Error getting storage node: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}
	if node.Uuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrNotFound),
		}, nil
	}

	// Only account owner can attach storage nodes to workspaces
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

	if account.UserId != userId {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrForbidden),
		}, nil
	}

	storageNodeWorkspaceTable := os.Getenv("STORAGE_NODE_WORKSPACE_TABLE")
	wsStore := store_dynamodb.NewStorageNodeWorkspaceStore(dynamoDBClient, storageNodeWorkspaceTable)

	// Check if already attached
	existing, err := wsStore.Get(ctx, nodeId, req.WorkspaceId)
	if err == nil && existing.StorageNodeUuid != "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusConflict,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrRecordAlreadyExists),
		}, nil
	}

	enablement := models.DynamoDBStorageNodeWorkspace{
		StorageNodeUuid: nodeId,
		WorkspaceId:     req.WorkspaceId,
		IsDefault:       req.IsDefault,
		EnabledBy:       userId,
		EnabledAt:       time.Now().UTC().Format(time.RFC3339),
	}

	err = wsStore.Insert(ctx, enablement)
	if err != nil {
		log.Printf("Error attaching storage node to workspace: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	m, err := json.Marshal(models.StorageNodeResponse{
		Message: "Storage node attached to workspace",
	})
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMarshaling),
		}, nil
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusCreated,
		Body:       string(m),
	}, nil
}
