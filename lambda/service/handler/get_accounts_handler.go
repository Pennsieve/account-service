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
	"github.com/pennsieve/account-service/service/models"
	"github.com/pennsieve/account-service/service/store_dynamodb"
)

func GetAccountsHandler(request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetAccountsHandler"
	ctx := context.Background()

	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrConfig),
		}, nil
	}
	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	accountsTable := os.Getenv("ACCOUNTS_TABLE")

	dynamo_store := store_dynamodb.NewAccountDatabaseStore(dynamoDBClient, accountsTable)
	dynamoAccounts, err := dynamo_store.Get(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrDynamoDB),
		}, nil
	}

	var accounts []models.Account
	for _, a := range dynamoAccounts {
		accounts = append(accounts, models.Account{
			Uuid:           a.Uuid,
			AccountId:      a.AccountId,
			AccountType:    a.AccountType,
			RoleName:       a.RoleName,
			ExternalId:     a.ExternalId,
			OrganizationId: a.OrganizationId,
			UserId:         a.UserId,
		})
	}

	m, err := json.Marshal(accounts)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrMarshaling),
		}, nil
	}
	response := events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(m),
	}
	return response, nil
}
