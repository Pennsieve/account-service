package mappers

import (
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
)

func DynamoDBAccountToJsonAccount(dynamoAccounts []store_dynamodb.Account) []models.Account {
	accounts := []models.Account{}

	for _, a := range dynamoAccounts {
		accounts = append(accounts, models.Account{
			Uuid:        a.Uuid,
			AccountId:   a.AccountId,
			AccountType: a.AccountType,
			RoleName:    a.RoleName,
			ExternalId:  a.ExternalId,
			UserId:      a.UserId,
			Name:        a.Name,
			Description: a.Description,
			Status:      a.Status,
		})
	}

	return accounts
}
