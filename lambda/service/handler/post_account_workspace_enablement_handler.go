package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/account-service/service/models"
	"github.com/pennsieve/account-service/service/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

type WorkspaceEnablementRequest struct {
	IsPublic bool `json:"isPublic"`
}

func PostAccountWorkspaceEnablementHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PostAccountWorkspaceEnablementHandler"
	
	// Get account UUID from path parameters
	accountUuid := request.PathParameters["uuid"]
	if accountUuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       handlerError(handlerName, ErrMissingAccountUuid),
		}, nil
	}

	var enablementRequest WorkspaceEnablementRequest
	if err := json.Unmarshal([]byte(request.Body), &enablementRequest); err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrUnmarshaling),
		}, nil
	}

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	organizationId := claims.OrgClaim.NodeId
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
	enablementTable := os.Getenv("ACCOUNT_WORKSPACE_ENABLEMENT_TABLE")
	
	// Verify that the account exists and belongs to the user
	accountsStore := &store_dynamodb.AccountDatabaseStore{
		DB:        dynamoDBClient,
		TableName: accountsTable,
	}
	
	account, err := accountsStore.GetById(ctx, accountUuid)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrDynamoDB),
		}, nil
	}
	
	if account.Uuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       handlerError(handlerName, ErrAccountNotFound),
		}, nil
	}
	
	if account.UserId != userId {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       handlerError(handlerName, ErrAccountDoesNotBelongToUser),
		}, nil
	}
	
	// Check if enablement already exists
	enablementStore := store_dynamodb.NewWorkspaceEnablementDatabaseStore(dynamoDBClient, enablementTable)
	existingEnablement, err := enablementStore.Get(ctx, accountUuid, organizationId)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrDynamoDB),
		}, nil
	}
	
	if existingEnablement.AccountUuid != "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusUnprocessableEntity,
			Body:       handlerError(handlerName, ErrAccountAlreadyEnabledForWorkspace),
		}, nil
	}
	
	// Create workspace enablement
	enablement := store_dynamodb.AccountWorkspaceEnablement{
		AccountUuid:    accountUuid,
		OrganizationId: organizationId,
		IsPublic:       enablementRequest.IsPublic,
		EnabledBy:      userId,
		EnabledAt:      time.Now().Unix(),
	}
	
	err = enablementStore.Insert(ctx, enablement)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrDynamoDB),
		}, nil
	}
	
	// Return the enablement
	m, err := json.Marshal(models.AccountWorkspaceEnablement{
		AccountUuid:    enablement.AccountUuid,
		OrganizationId: enablement.OrganizationId,
		IsPublic:       enablement.IsPublic,
		EnabledBy:      enablement.EnabledBy,
		EnabledAt:      enablement.EnabledAt,
	})
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrMarshaling),
		}, nil
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusCreated,
		Body:       string(m),
	}, nil
}