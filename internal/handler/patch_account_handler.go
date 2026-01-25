package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
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
			Body:       handlerError(handlerName, ErrMissingAccountUuid),
		}, nil
	}

	// Parse request body
	var updateReq AccountUpdateRequest
	if err := json.Unmarshal([]byte(request.Body), &updateReq); err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       handlerError(handlerName, ErrUnmarshaling),
		}, nil
	}

	// Validate status value if provided
	if updateReq.Status != nil && *updateReq.Status != "Enabled" && *updateReq.Status != "Paused" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       handlerError(handlerName, ErrInvalidStatus),
		}, nil
	}
	
	// Check that at least one field is being updated
	if updateReq.Status == nil && updateReq.Name == nil && updateReq.Description == nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       handlerError(handlerName, ErrUnmarshaling),
		}, nil
	}

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	userId := claims.UserClaim.NodeId

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrConfig),
		}, nil
	}

	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	accountsTable := os.Getenv("ACCOUNTS_TABLE")
	accountsStore := &store_dynamodb.AccountDatabaseStore{
		DB:        dynamoDBClient,
		TableName: accountsTable,
	}

	// Get the account to verify ownership
	account, err := accountsStore.GetById(ctx, accountUuid)
	if err != nil {
		log.Printf("error getting account: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrDynamoDB),
		}, nil
	}

	// Check if account exists
	if account.Uuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       handlerError(handlerName, ErrNotFound),
		}, nil
	}

	// Check if user owns this account
	if account.UserId != userId {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       handlerError(handlerName, ErrAccountDoesNotBelongToUser),
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
	
	err = accountsStore.Update(ctx, account)
	if err != nil {
		log.Printf("error updating account: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrDynamoDB),
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
			Body:       handlerError(handlerName, ErrMarshaling),
		}, nil
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(m),
	}, nil
}