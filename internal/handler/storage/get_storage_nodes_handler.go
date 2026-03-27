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

// GetStorageNodesHandler lists storage nodes
// GET /storage-nodes?organization_id=X or GET /storage-nodes?account_owner=true
func GetStorageNodesHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetStorageNodesHandler"

	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	dynamoDBClient := dynamodb.NewFromConfig(cfg)

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	userId := claims.UserClaim.NodeId

	organizationId := request.QueryStringParameters["organization_id"]
	accountOwnerMode := request.QueryStringParameters["account_owner"] == "true"

	storageNodesTable := os.Getenv("STORAGE_NODES_TABLE")
	storageNodeStore := store_dynamodb.NewStorageNodeDatabaseStore(dynamoDBClient, storageNodesTable)

	storageNodeWorkspaceTable := os.Getenv("STORAGE_NODE_WORKSPACE_TABLE")
	workspaceStore := store_dynamodb.NewStorageNodeWorkspaceStore(dynamoDBClient, storageNodeWorkspaceTable)

	var storageNodes []models.DynamoDBStorageNode

	if accountOwnerMode {
		// Return all storage nodes on accounts owned by the user
		accountsTable := os.Getenv("ACCOUNTS_TABLE")
		accountStore := store_dynamodb.NewAccountDatabaseStore(dynamoDBClient, accountsTable)
		accountStoreImpl, ok := accountStore.(*store_dynamodb.AccountDatabaseStore)
		if !ok {
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
			}, nil
		}

		userAccounts, err := accountStoreImpl.GetByUserId(ctx, userId)
		if err != nil {
			log.Printf("Error getting user accounts: %v", err)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
			}, nil
		}

		for _, account := range userAccounts {
			accountNodes, err := storageNodeStore.GetByAccount(ctx, account.Uuid)
			if err != nil {
				log.Printf("Error fetching storage nodes for account %s: %v", account.Uuid, err)
				continue
			}
			storageNodes = append(storageNodes, accountNodes...)
		}
	} else if organizationId != "" {
		// Return storage nodes associated with the workspace
		if validationResponse := utils.ValidateOrganizationMembership(ctx, cfg, userId, organizationId, handlerName); validationResponse != nil {
			return *validationResponse, nil
		}

		workspaceEnablements, err := workspaceStore.GetByWorkspace(ctx, organizationId)
		if err != nil {
			log.Printf("Error getting workspace enablements: %v", err)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
			}, nil
		}

		for _, enablement := range workspaceEnablements {
			node, err := storageNodeStore.GetById(ctx, enablement.StorageNodeUuid)
			if err != nil {
				log.Printf("Error fetching storage node %s: %v", enablement.StorageNodeUuid, err)
				continue
			}
			if node.Uuid != "" {
				storageNodes = append(storageNodes, node)
			}
		}
	} else {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrBadRequest),
		}, nil
	}

	// Build response with workspace info
	var responseNodes []models.StorageNode
	for _, node := range storageNodes {
		workspaces, err := workspaceStore.GetByStorageNode(ctx, node.Uuid)
		if err != nil {
			log.Printf("Error getting workspaces for storage node %s: %v", node.Uuid, err)
		}

		var wsEnablements []models.StorageNodeWorkspaceEnablement
		for _, ws := range workspaces {
			wsEnablements = append(wsEnablements, models.StorageNodeWorkspaceEnablement{
				StorageNodeUuid: ws.StorageNodeUuid,
				WorkspaceId:     ws.WorkspaceId,
				IsDefault:       ws.IsDefault,
				EnabledBy:       ws.EnabledBy,
				EnabledAt:       ws.EnabledAt,
			})
		}

		responseNodes = append(responseNodes, models.StorageNode{
			Uuid:            node.Uuid,
			Name:            node.Name,
			Description:     node.Description,
			AccountUuid:     node.AccountUuid,
			StorageLocation: node.StorageLocation,
			Region:          node.Region,
			ProviderType:    node.ProviderType,
			Status:          node.Status,
			CreatedAt:       node.CreatedAt,
			CreatedBy:       node.CreatedBy,
			Workspaces:      wsEnablements,
		})
	}

	if responseNodes == nil {
		responseNodes = []models.StorageNode{}
	}

	m, err := json.Marshal(responseNodes)
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
