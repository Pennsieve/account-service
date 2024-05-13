package mappers

import (
	"github.com/pennsieve/account-service/service/models"
	"github.com/pennsieve/account-service/service/store_dynamodb"
)

func DynamoDBAccountToJsonAccount(dynamoAccounts []store_dynamodb.Account) []models.Account {
	accounts := []models.Account{}

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

	return accounts
}
