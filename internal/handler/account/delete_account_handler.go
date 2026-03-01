package account

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
)

// DeleteAccountHandler deletes an account and its workspace enablements
// DELETE /accounts/{id}
//
// Query Parameters:
// - force: if "true", delete even if account has active compute nodes
//
// Required Permissions:
// - Must be the owner of the account (account.UserId == requestingUserId)
func DeleteAccountHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "DeleteAccountHandler"

	uuid := request.PathParameters["id"]
	if uuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.HandlerError(handlerName, errors.ErrMissingAccountUuid),
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
	nodesTable := os.Getenv("NODES_TABLE")
	enablementTable := os.Getenv("ACCOUNT_WORKSPACE_TABLE")

	accountsStore := &store_dynamodb.AccountDatabaseStore{
		DB:        dynamoDBClient,
		TableName: accountsTable,
	}

	// Get the account
	account, err := accountsStore.GetById(ctx, uuid)
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

	// Check ownership
	if account.UserId != userId {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       errors.HandlerError(handlerName, errors.ErrAccountDoesNotBelongToUser),
		}, nil
	}

	// Check for active compute nodes
	nodeStore := store_dynamodb.NewNodeDatabaseStore(dynamoDBClient, nodesTable)
	nodes, err := nodeStore.GetByAccount(ctx, uuid)
	if err != nil {
		log.Printf("error getting nodes for account %s: %v", uuid, err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	force := request.QueryStringParameters["force"]
	if len(nodes) > 0 && force != "true" {
		body, _ := json.Marshal(map[string]interface{}{
			"error":     fmt.Sprintf("account has %d active compute nodes", len(nodes)),
			"nodeCount": len(nodes),
		})
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusConflict,
			Body:       string(body),
		}, nil
	}

	// Delete workspace enablements
	enablementStore := store_dynamodb.NewAccountWorkspaceStore(dynamoDBClient, enablementTable)
	enablements, err := enablementStore.GetByAccount(ctx, uuid)
	if err != nil {
		log.Printf("warning: failed to get workspace enablements for account %s: %v", uuid, err)
	} else {
		for _, e := range enablements {
			if err := enablementStore.Delete(ctx, e.AccountUuid, e.WorkspaceId); err != nil {
				log.Printf("warning: failed to delete workspace enablement %s/%s: %v", e.AccountUuid, e.WorkspaceId, err)
			}
		}
	}

	// Delete the account record
	err = accountsStore.Delete(ctx, uuid)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	// Return the deleted account info so the agent can clean up the IAM role
	m, err := json.Marshal(models.Account{
		Uuid:        account.Uuid,
		AccountId:   account.AccountId,
		AccountType: account.AccountType,
		RoleName:    account.RoleName,
		ExternalId:  account.ExternalId,
		UserId:      account.UserId,
	})
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