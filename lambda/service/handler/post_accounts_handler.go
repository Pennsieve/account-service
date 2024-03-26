package handler

import (
	"context"
	"encoding/json"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/google/uuid"
	"github.com/pennsieve/account-service/service/models"
	"github.com/pennsieve/account-service/service/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

func PostAccountsHandler(request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PostAccountsHandler"
	var account models.Account
	if err := json.Unmarshal([]byte(request.Body), &account); err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 500,
			Body:       handlerError(handlerName, ErrUnmarshaling),
		}, nil
	}

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	organizationId := claims.OrgClaim.NodeId
	userId := claims.UserClaim.NodeId

	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 500,
			Body:       handlerError(handlerName, ErrConfig),
		}, nil
	}
	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	accountsTable := os.Getenv("ACCOUNTS_TABLE")
	log.Println(accountsTable)
	accountsStore := store_dynamodb.NewAccountDatabaseStore(dynamoDBClient, accountsTable)

	id := uuid.New()
	registeredAccountId := id.String()

	// persist to dynamodb
	store_integration := store_dynamodb.Account{
		Uuid:           registeredAccountId,
		UserId:         userId,
		OrganizationId: organizationId,
		AccountId:      account.AccountId,
		AccountType:    account.AccountType,
		RoleName:       account.RoleName,
		ExternalId:     account.ExternalId,
	}
	err = accountsStore.Insert(context.Background(), store_integration)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 500,
			Body:       handlerError(handlerName, ErrDynamoDB),
		}, nil
	}

	return events.APIGatewayV2HTTPResponse{}, nil
}
