package account

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
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
	"github.com/pennsieve/account-service/internal/errors"
)

type AccountUpdateRequest struct {
	Status      *string `json:"status,omitempty"`
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

func PatchAccountHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PatchAccountHandler"
	
	// Get account UUID from path
	accountUuid := request.PathParameters["id"]
	if accountUuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.HandlerError(handlerName, errors.ErrMissingAccountUuid),
		}, nil
	}

	// Parse request body
	var updateReq AccountUpdateRequest
	if err := json.Unmarshal([]byte(request.Body), &updateReq); err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.HandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}

	// Validate status value if provided
	if updateReq.Status != nil && *updateReq.Status != "Enabled" && *updateReq.Status != "Paused" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.HandlerError(handlerName, errors.ErrInvalidStatus),
		}, nil
	}
	
	// Check that at least one field is being updated
	if updateReq.Status == nil && updateReq.Name == nil && updateReq.Description == nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.HandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	userId := claims.UserClaim.NodeId

	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	accountsTable := os.Getenv("ACCOUNTS_TABLE")
	accountsStore := &store_dynamodb.AccountDatabaseStore{
		DB:        dynamoDBClient,
		TableName: accountsTable,
	}
	
	// Initialize node store for updating compute nodes
	nodesTable := os.Getenv("NODES_TABLE")
	nodesStore := store_dynamodb.NewNodeDatabaseStore(dynamoDBClient, nodesTable)

	// Get the account to verify ownership
	account, err := accountsStore.GetById(ctx, accountUuid)
	if err != nil {
		log.Printf("error getting account: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	// Check if account exists
	if account.Uuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.HandlerError(handlerName, errors.ErrNotFound),
		}, nil
	}

	// Check if user owns this account
	if account.UserId != userId {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       errors.HandlerError(handlerName, errors.ErrAccountDoesNotBelongToUser),
		}, nil
	}

	// Update only the fields that were provided
	if updateReq.Status != nil {
		account.Status = *updateReq.Status
	}
	if updateReq.Name != nil {
		account.Name = *updateReq.Name
	}
	if updateReq.Description != nil {
		account.Description = *updateReq.Description
	}
	
	// If account status is being set to "Paused", pause all compute nodes
	if updateReq.Status != nil && *updateReq.Status == "Paused" {
		// Get all compute nodes for this account
		nodes, err := nodesStore.GetByAccount(ctx, accountUuid)
		if err != nil {
			log.Printf("warning: failed to get compute nodes for account %s: %v", accountUuid, err)
			// Continue with account update even if we can't pause nodes
		} else {
			// Update status of all nodes to "Paused"
			for _, node := range nodes {
				if node.Status != "Paused" {
					err := nodesStore.UpdateStatus(ctx, node.Uuid, "Paused")
					if err != nil {
						log.Printf("warning: failed to pause compute node %s: %v", node.Uuid, err)
						// Continue updating other nodes even if one fails
					}
				}
			}
		}
	}
	// Note: When account is set to "Enabled", we don't automatically enable compute nodes
	// as per the requirement
	
	err = accountsStore.Update(ctx, account)
	if err != nil {
		log.Printf("error updating account: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	// Return updated account
	response := models.Account{
		Uuid:        account.Uuid,
		AccountId:   account.AccountId,
		AccountType: account.AccountType,
		RoleName:    account.RoleName,
		ExternalId:  account.ExternalId,
		UserId:      account.UserId,
		Name:        account.Name,
		Description: account.Description,
		Status:      account.Status,
	}

	m, err := json.Marshal(response)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrMarshaling),
		}, nil
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(m),
	}, nil
}