package mappers

import (
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
)

func DynamoDBNodeToJsonNode(dynamoNodes []models.DynamoDBNode) []models.Node {
	nodes := []models.Node{}

	for _, c := range dynamoNodes {
		nodes = append(nodes, models.Node{
			Uuid:                  c.Uuid,
			Name:                  c.Name,
			Description:           c.Description,
			ComputeNodeGatewayUrl: c.ComputeNodeGatewayUrl,
			EfsId:                 c.EfsId,
			QueueUrl:              c.QueueUrl,
			Account: models.NodeAccount{
				Uuid:        c.AccountUuid,
				AccountId:   c.AccountId,
				AccountType: c.AccountType,
			},
			CreatedAt:          c.CreatedAt,
			OrganizationId:     c.OrganizationId,
			UserId:             c.UserId,
			Identifier:         c.Identifier,
			WorkflowManagerTag: c.WorkflowManagerTag,
		})
	}

	return nodes
}

func DynamoDBAccountToJsonAccount(dynamoAccounts []store_dynamodb.Account) []models.Account {
	accounts := []models.Account{}

	for _, c := range dynamoAccounts {
		accounts = append(accounts, models.Account{
			Uuid:        c.Uuid,
			AccountId:   c.AccountId,
			AccountType: c.AccountType,
			RoleName:    c.RoleName,
			ExternalId:  c.ExternalId,
			UserId:      c.UserId,
			Name:        c.Name,
			Description: c.Description,
			Status:      c.Status,
		})
	}

	return accounts
}