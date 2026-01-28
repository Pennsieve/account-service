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
	"github.com/pennsieve/account-service/internal/errors"
)

func GetAccountHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetAccountHandler"
	uuid := request.PathParameters["id"]

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

	dynamo_store := store_dynamodb.NewAccountDatabaseStore(dynamoDBClient, accountsTable)
	account, err := dynamo_store.GetById(ctx, uuid)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	if (store_dynamodb.Account{}) == account {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.HandlerError(handlerName, errors.ErrNoRecordsFound),
		}, nil
	}

	m, err := json.Marshal(models.Account{
		Uuid:        account.Uuid,
		AccountId:   account.AccountId,
		AccountType: account.AccountType,
		RoleName:    account.RoleName,
		ExternalId:  account.ExternalId,
		UserId:      account.UserId,
		Name:        account.Name,
		Description: account.Description,
		Status:      account.Status,
	})
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrMarshaling),
		}, nil
	}
	response := events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(m),
	}
	return response, nil
}
