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
	"github.com/google/uuid"
	"github.com/pennsieve/account-service/service/models"
	"github.com/pennsieve/account-service/service/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

func PostAccountsHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PostAccountsHandler"
	var account models.Account
	if err := json.Unmarshal([]byte(request.Body), &account); err != nil {
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
	accountsStore := store_dynamodb.NewAccountDatabaseStore(dynamoDBClient, accountsTable)

	// Get account(s) by organisationId/workspaceId and accountId
	queryParams := make(map[string]string)
	queryParams["accountId"] = account.AccountId
	accounts, err := accountsStore.Get(ctx, organizationId, queryParams)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrConfig),
		}, nil
	}
	if len(accounts) > 0 {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusUnprocessableEntity,
			Body:       handlerError(handlerName, ErrRecordAlreadyExists),
		}, nil
	}

	id := uuid.New()
	registeredAccountId := id.String()

	// persist to dynamodb
	store_account := store_dynamodb.Account{
		Uuid:           registeredAccountId,
		UserId:         userId,
		OrganizationId: organizationId,
		AccountId:      account.AccountId,
		AccountType:    account.AccountType,
		RoleName:       account.RoleName,
		ExternalId:     account.ExternalId,
	}
	err = accountsStore.Insert(ctx, store_account)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrDynamoDB),
		}, nil
	}

	m, err := json.Marshal(models.AccountResponse{
		Uuid: registeredAccountId,
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
