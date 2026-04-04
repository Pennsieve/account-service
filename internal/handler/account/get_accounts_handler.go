package account

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	authclient "github.com/pennsieve/account-service/internal/authorizer"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/mappers"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
)

// GetAccountsHandler retrieves all accounts owned by the current user
// GET /accounts
//
// Required Permissions:
// - Must be an authenticated user
// - Returns only accounts owned by the requesting user (filtered by userId)
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
	if workspaceFilter, ok := queryParams["organization_id"]; ok && workspaceFilter != "" {
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

		// Create maps for quick lookup
		enabledAccountUuids := make(map[string]bool)
		publicAccountUuids := make(map[string]bool)
		for _, enablement := range workspaceEnablements {
			enabledAccountUuids[enablement.AccountUuid] = true
			if enablement.IsPublic {
				publicAccountUuids[enablement.AccountUuid] = true
			}
		}

		// Filter user's own accounts by enabled accounts
		var filteredAccounts []store_dynamodb.Account
		ownedUuids := make(map[string]bool)
		for _, account := range userAccounts {
			if enabledAccountUuids[account.Uuid] {
				filteredAccounts = append(filteredAccounts, account)
				ownedUuids[account.Uuid] = true
			}
		}

		// If there are public accounts the user doesn't own, check if user is an org admin
		if len(publicAccountUuids) > 0 {
			hasPublicNotOwned := false
			for uuid := range publicAccountUuids {
				if !ownedUuids[uuid] {
					hasPublicNotOwned = true
					break
				}
			}

			if hasPublicNotOwned {
				lambdaClient := lambda.NewFromConfig(cfg)
				auth := authclient.NewLambdaDirectAuthorizer(lambdaClient)
				authResp, err := auth.Authorize(ctx, userId, workspaceFilter)
				if err == nil && authResp.IsAuthorized && authclient.IsOrgAdmin(authResp.Claims) {
					// Admin can see public accounts they don't own
					for uuid := range publicAccountUuids {
						if !ownedUuids[uuid] {
							account, err := accountStore.GetById(ctx, uuid)
							if err == nil && account.Uuid != "" {
								filteredAccounts = append(filteredAccounts, account)
							}
						}
					}
				}
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
					EnableCompute:  e.EnableCompute,
					EnableStorage:  e.EnableStorage,
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
