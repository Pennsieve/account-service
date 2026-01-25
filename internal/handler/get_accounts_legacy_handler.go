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
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

// GetAccountsLegacyHandler provides backward compatibility during migration
// It returns accounts from both old (organizationId in account) and new (workspace enablement) models
func GetAccountsLegacyHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "GetAccountsLegacyHandler"
	queryParams := request.QueryStringParameters

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
	enablementTable := os.Getenv("ACCOUNT_WORKSPACE_TABLE")

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	organizationId := claims.OrgClaim.NodeId

	accountStore := store_dynamodb.NewAccountDatabaseStore(dynamoDBClient, accountsTable)

	// First, get old-style accounts (pre-migration)
	oldStyleAccounts, err := accountStore.Get(ctx, organizationId, queryParams)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrDynamoDB),
		}, nil
	}

	// Then, get new-style accounts (post-migration) through workspace enablement
	var newStyleAccounts []store_dynamodb.Account
	if enablementTable != "" {
		enablementStore := store_dynamodb.NewAccountWorkspaceStore(dynamoDBClient, enablementTable)
		enablements, err := enablementStore.GetByWorkspace(ctx, organizationId)
		if err != nil {
			log.Printf("Error getting workspace enablements: %v", err)
			// Don't fail the entire request if we can't get enablements
		} else {
			// For each enablement, fetch the corresponding account
			accountUuids := make(map[string]bool)
			for _, enablement := range enablements {
				// Skip if we already have this account from old-style query
				alreadyHave := false
				for _, oldAccount := range oldStyleAccounts {
					if oldAccount.Uuid == enablement.AccountUuid {
						alreadyHave = true
						break
					}
				}
				if alreadyHave {
					continue
				}

				// Avoid duplicates from multiple enablements
				if accountUuids[enablement.AccountUuid] {
					continue
				}
				accountUuids[enablement.AccountUuid] = true

				account, err := accountStore.GetById(ctx, enablement.AccountUuid)
				if err != nil {
					log.Printf("Error getting account %s: %v", enablement.AccountUuid, err)
					continue
				}
				if account.Uuid != "" {
					// Apply accountId filter if specified
					if accountId, found := queryParams["accountId"]; found && account.AccountId != accountId {
						continue
					}
					newStyleAccounts = append(newStyleAccounts, account)
				}
			}
		}
	}

	// Combine both lists
	allAccounts := append(oldStyleAccounts, newStyleAccounts...)

	// Convert to response format
	var responseAccounts []models.Account
	for _, account := range allAccounts {
		responseAccounts = append(responseAccounts, models.Account{
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
	}

	m, err := json.Marshal(responseAccounts)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       handlerError(handlerName, ErrMarshaling),
		}, nil
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(m),
	}, nil
}
