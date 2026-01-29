package account

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/account-service/internal/utils"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
	"github.com/pennsieve/account-service/internal/errors"
)

// WorkspaceEnablementRequest contains the parameters for enabling a workspace on an account
type WorkspaceEnablementRequest struct {
	// IsPublic determines who can create compute nodes on this account:
	// - true: workspace managers can create compute nodes on this account
	// - false: only the account owner can create compute nodes on this account
	IsPublic bool `json:"isPublic"`
}

func PostAccountWorkspaceEnablementHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PostAccountWorkspaceEnablementHandler"

	// Get account UUID from path parameters
	accountUuid := request.PathParameters["uuid"]
	if accountUuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.HandlerError(handlerName, errors.ErrMissingAccountUuid),
		}, nil
	}

	var enablementRequest WorkspaceEnablementRequest
	if err := json.Unmarshal([]byte(request.Body), &enablementRequest); err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.HandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}

	// Get userId using test-aware function
	userId, err := utils.GetUserIdFromRequest(request)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	// Get organizationId from request
	var organizationId string
	envValue := os.Getenv("ENV")
	if envValue == "DOCKER" || envValue == "TEST" {
		// In test mode, get organizationId from environment variable
		organizationId = os.Getenv("TEST_ORGANIZATION_ID")
		if organizationId == "" {
			organizationId = "test-org-default"
		}
	} else {
		// Production mode: parse authorization claims
		claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
		organizationId = claims.OrgClaim.NodeId
	}

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
	enablementTable := os.Getenv("ACCOUNT_WORKSPACE_TABLE")

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
			Body:       errors.HandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	if account.Uuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.HandlerError(handlerName, errors.ErrAccountNotFound),
		}, nil
	}

	if account.UserId != userId {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       errors.HandlerError(handlerName, errors.ErrAccountDoesNotBelongToUser),
		}, nil
	}

	// Check if enablement already exists
	enablementStore := store_dynamodb.NewAccountWorkspaceStore(dynamoDBClient, enablementTable)
	existingEnablement, err := enablementStore.Get(ctx, accountUuid, organizationId)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	if existingEnablement.AccountUuid != "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusUnprocessableEntity,
			Body:       errors.HandlerError(handlerName, errors.ErrAccountAlreadyEnabledForWorkspace),
		}, nil
	}

	// Create workspace enablement
	enablement := store_dynamodb.AccountWorkspace{
		AccountUuid:    accountUuid,
		WorkspaceId:    organizationId,  // Map organizationId to WorkspaceId for DB
		IsPublic:       enablementRequest.IsPublic,
		EnabledBy:      userId,
		EnabledAt:      time.Now().Unix(),
	}

	err = enablementStore.Insert(ctx, enablement)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	// Return the enablement
	m, err := json.Marshal(models.AccountWorkspaceEnablement{
		AccountUuid:    enablement.AccountUuid,
		OrganizationId: organizationId,  // Use the original organizationId
		IsPublic:       enablement.IsPublic,
		EnabledBy:      enablement.EnabledBy,
		EnabledAt:      enablement.EnabledAt,
	})
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrMarshaling),
		}, nil
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusCreated,
		Body:       string(m),
	}, nil
}
