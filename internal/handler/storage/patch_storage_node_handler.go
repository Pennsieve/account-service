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
	"github.com/pennsieve/account-service/internal/service"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

// PatchStorageNodeHandler updates a storage node's name, description, or status
// PATCH /storage-nodes/{id}
func PatchStorageNodeHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PatchStorageNodeHandler"
	nodeId := request.PathParameters["id"]

	if nodeId == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrBadRequest),
		}, nil
	}

	var updateReq models.StorageNodeUpdateRequest
	if err := json.Unmarshal([]byte(request.Body), &updateReq); err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}

	if updateReq.Name == nil && updateReq.Description == nil && updateReq.Status == nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrBadRequest),
		}, nil
	}

	if updateReq.Status != nil && *updateReq.Status != "Enabled" && *updateReq.Status != "Disabled" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrInvalidStatus),
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

	// Check permissions: account owner or workspace admin (if IsPublic)
	canUpdate := false
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

	canUpdate = account.UserId == userId
	if !canUpdate {
		canUpdate = canAdminManageNode(ctx, cfg, dynamoDBClient, userId, node.Uuid, node.AccountUuid)
	}
	if !canUpdate {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrForbidden),
		}, nil
	}

	oldStatus := node.Status

	if updateReq.Name != nil {
		node.Name = *updateReq.Name
	}
	if updateReq.Description != nil {
		node.Description = *updateReq.Description
	}
	if updateReq.Status != nil {
		node.Status = *updateReq.Status
	}

	err = storageNodeStore.Put(ctx, node)
	if err != nil {
		log.Printf("Error updating storage node: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	// Regenerate policies if status changed (Enabled↔Disabled affects which buckets are in the policy)
	if node.ProviderType == "s3" && oldStatus != node.Status {
		storagePolicyService := service.NewStoragePolicyService(cfg, storageNodeStore)
		if err := storagePolicyService.RegenerateStoragePolicies(ctx); err != nil {
			log.Printf("Warning: Failed to regenerate storage policies: %v", err)
		}
	}

	response := models.StorageNode{
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
