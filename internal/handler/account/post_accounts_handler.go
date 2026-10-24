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
	"github.com/google/uuid"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/errors"
)

// PostAccountsHandler creates a new account
// POST /accounts
//
// Required Permissions:
// - Must be an authenticated user
// - Account will be associated with the requesting user (userId)
// - Prevents duplicate account registration per user
// - Pennsieve must be able to assume the role before it's registered; with an
//   external ID (agents that asked for one), the role must also require it, and
//   the account's role can't already be registered by anyone.
func PostAccountsHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PostAccountsHandler"
	var account models.Account
	if err := json.Unmarshal([]byte(request.Body), &account); err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.HandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}

	if !validExternalId(account.ExternalId) {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.HandlerError(handlerName, ErrInvalidExternalId),
		}, nil
	}

	userId, err := utils.GetUserIdFromRequest(request)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrConfig),
		}, nil
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
	accountsStore := &store_dynamodb.AccountDatabaseStore{
		DB:        dynamoDBClient,
		TableName: accountsTable,
	}

	// Check if user already has this account registered
	userAccounts, err := accountsStore.GetByUserId(ctx, userId)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrConfig),
		}, nil
	}
	
	// Check for duplicate account for this user
	for _, existingAccount := range userAccounts {
		if existingAccount.AccountId == account.AccountId {
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusUnprocessableEntity,
				Body:       errors.HandlerError(handlerName, errors.ErrRecordAlreadyExists),
			}, nil
		}
	}

	// One registration per role once roles carry an external ID: another
	// user registering the same account and role is refused, rather than
	// handed a second record for it.
	if account.ExternalId != "" {
		sameAccount, err := accountsStore.GetByAccountId(ctx, account.AccountId)
		if err != nil {
			log.Println(err.Error())
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.HandlerError(handlerName, errors.ErrDynamoDB),
			}, nil
		}
		for _, existing := range sameAccount {
			if existing.RoleName == account.RoleName {
				return events.APIGatewayV2HTTPResponse{
					StatusCode: http.StatusConflict,
					Body:       errors.HandlerError(handlerName, errors.ErrRecordAlreadyExists),
				}, nil
			}
		}
	}

	if err := verifyRole(ctx, NewRoleAssumer(cfg), account.AccountId, account.RoleName, account.ExternalId, time.Sleep); err != nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.HandlerError(handlerName, err),
		}, nil
	}

	id := uuid.New()
	registeredAccountId := id.String()

	// persist to dynamodb - now only associated with user
	store_account := store_dynamodb.Account{
		Uuid:        registeredAccountId,
		UserId:      userId,
		AccountId:   account.AccountId,
		AccountType: account.AccountType,
		RoleName:    account.RoleName,
		ExternalId:  account.ExternalId,
		Name:        account.Name,
		Description: account.Description,
		Status:      "Enabled", // Default status is Enabled
	}
	err = accountsStore.Insert(ctx, store_account)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	m, err := json.Marshal(models.AccountResponse{
		Uuid: registeredAccountId,
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
