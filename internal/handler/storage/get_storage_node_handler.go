package storage

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
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

// GetStorageNodeHandler retrieves a single storage node by ID
// GET /storage-nodes/{id}
func GetStorageNodeHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetStorageNodeHandler"
	nodeId := request.PathParameters["id"]

	if nodeId == "" {
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

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	userId := claims.UserClaim.NodeId
	organizationId := claims.OrgClaim.NodeId

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

	// Check access: account owner or workspace member
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

	hasAccess := account.UserId == userId
	if !hasAccess && organizationId != "" {
		// Check if storage node is associated with user's workspace
		storageNodeWorkspaceTable := os.Getenv("STORAGE_NODE_WORKSPACE_TABLE")
		wsStore := store_dynamodb.NewStorageNodeWorkspaceStore(dynamoDBClient, storageNodeWorkspaceTable)
		ws, err := wsStore.Get(ctx, nodeId, organizationId)
		if err == nil && ws.StorageNodeUuid != "" {
			hasAccess = true
		}
	}

	if !hasAccess {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrForbidden),
		}, nil
	}

	// Get workspace associations
	storageNodeWorkspaceTable := os.Getenv("STORAGE_NODE_WORKSPACE_TABLE")
	wsStore := store_dynamodb.NewStorageNodeWorkspaceStore(dynamoDBClient, storageNodeWorkspaceTable)

	var wsEnablements []models.StorageNodeWorkspaceEnablement
	if account.UserId == userId {
		// Owner sees all workspace attachments
		workspaces, _ := wsStore.GetByStorageNode(ctx, nodeId)
		for _, ws := range workspaces {
			wsEnablements = append(wsEnablements, models.StorageNodeWorkspaceEnablement{
				StorageNodeUuid: ws.StorageNodeUuid,
				WorkspaceId:     ws.WorkspaceId,
				IsDefault:       ws.IsDefault,
				EnabledBy:       ws.EnabledBy,
				EnabledAt:       ws.EnabledAt,
			})
		}
	} else if organizationId != "" {
		// Non-owner sees only their workspace
		ws, err := wsStore.Get(ctx, nodeId, organizationId)
		if err == nil && ws.StorageNodeUuid != "" {
			wsEnablements = append(wsEnablements, models.StorageNodeWorkspaceEnablement{
				StorageNodeUuid: ws.StorageNodeUuid,
				WorkspaceId:     ws.WorkspaceId,
				IsDefault:       ws.IsDefault,
				EnabledBy:       ws.EnabledBy,
				EnabledAt:       ws.EnabledAt,
			})
		}
	}

	response := models.StorageNode{
		Uuid:            node.Uuid,
		Name:            node.Name,
		Description:     node.Description,
		AccountUuid:     node.AccountUuid,
		AccountName:     account.Name,
		AccountOwnerId:  account.UserId,
		StorageLocation: node.StorageLocation,
		Region:          node.Region,
		ProviderType:    node.ProviderType,
		Status:          node.Status,
		CreatedAt:       node.CreatedAt,
		CreatedBy:       node.CreatedBy,
		Workspaces:      wsEnablements,
	}

	m, err := json.Marshal(response)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMarshaling),
		}, nil
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(m),
	}, nil
}
