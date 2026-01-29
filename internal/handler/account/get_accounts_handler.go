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
	"github.com/pennsieve/account-service/internal/mappers"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/errors"
)

func GetAccountsHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetAccountsHandler"
	queryParams := request.QueryStringParameters

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

	userId, err := utils.GetUserIdFromRequest(request)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	// Get user's accounts
	accountStore := &store_dynamodb.AccountDatabaseStore{
		DB:        dynamoDBClient,
		TableName: accountsTable,
	}
	userAccounts, err := accountStore.GetByUserId(ctx, userId)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.HandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	// If workspace filter is provided, filter accounts by workspace enablement
	if workspaceFilter, ok := queryParams["workspace"]; ok && workspaceFilter != "" {
		enablementStore := store_dynamodb.NewAccountWorkspaceStore(dynamoDBClient, enablementTable)

		// Get all enablements for the workspace
		workspaceEnablements, err := enablementStore.GetByWorkspace(ctx, workspaceFilter)
		if err != nil {
			log.Println(err.Error())
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.HandlerError(handlerName, errors.ErrDynamoDB),
			}, nil
		}

		// Create map for quick lookup
		enabledAccountUuids := make(map[string]bool)
		for _, enablement := range workspaceEnablements {
			enabledAccountUuids[enablement.AccountUuid] = true
		}

		// Filter user accounts by enabled accounts
		var filteredAccounts []store_dynamodb.Account
		for _, account := range userAccounts {
			if enabledAccountUuids[account.Uuid] {
				filteredAccounts = append(filteredAccounts, account)
			}
		}
		userAccounts = filteredAccounts
	}

	// Include workspaces information if requested
	if includeWorkspaces, ok := queryParams["includeWorkspaces"]; ok && includeWorkspaces == "true" {
		enablementStore := store_dynamodb.NewAccountWorkspaceStore(dynamoDBClient, enablementTable)

		var accountsWithWorkspaces []models.AccountWithWorkspaces
		for _, account := range userAccounts {
			enablements, err := enablementStore.GetByAccount(ctx, account.Uuid)
			if err != nil {
				log.Println(err.Error())
				// Continue without enablements rather than failing
				enablements = []store_dynamodb.AccountWorkspace{}
			}

			modelEnablements := make([]models.AccountWorkspaceEnablement, len(enablements))
			for i, e := range enablements {
				modelEnablements[i] = models.AccountWorkspaceEnablement{
					AccountUuid:    e.AccountUuid,
					OrganizationId: e.WorkspaceId,  // WorkspaceId from DB maps to OrganizationId in model
					IsPublic:       e.IsPublic,
					EnabledBy:      e.EnabledBy,
					EnabledAt:      e.EnabledAt,
				}
			}

			accountsWithWorkspaces = append(accountsWithWorkspaces, models.AccountWithWorkspaces{
				Account: models.Account{
					Uuid:        account.Uuid,
					AccountId:   account.AccountId,
					AccountType: account.AccountType,
					RoleName:    account.RoleName,
					ExternalId:  account.ExternalId,
					UserId:      account.UserId,
					Name:        account.Name,
					Description: account.Description,
					Status:      account.Status,
				},
				EnabledWorkspaces: modelEnablements,
			})
		}

		m, err := json.Marshal(accountsWithWorkspaces)
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

	// Return simple account list
	m, err := json.Marshal(mappers.DynamoDBAccountToJsonAccount(userAccounts))
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
