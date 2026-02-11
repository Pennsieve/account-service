package mappers

import (
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
)

// DynamoDBNodeToJsonNodeWithAccountStatus converts DynamoDB nodes to JSON nodes with account status override
// If the account is Paused and the node is not Pending, the node status will be overridden to Paused
func DynamoDBNodeToJsonNodeWithAccountStatus(dynamoNodes []models.DynamoDBNode, accountStatusMap map[string]string) []models.Node {
	// Use the new function with empty owner map for backwards compatibility
	return DynamoDBNodeToJsonNodeWithAccountInfo(dynamoNodes, accountStatusMap, nil)
}

// DynamoDBNodeToJsonNodeWithAccountInfo converts DynamoDB nodes to JSON nodes with account status override and owner info
// If the account is Paused and the node is not Pending, the node status will be overridden to Paused
// Also includes the account owner's userId if available
func DynamoDBNodeToJsonNodeWithAccountInfo(dynamoNodes []models.DynamoDBNode, accountStatusMap map[string]string, accountOwnerMap map[string]string) []models.Node {
	nodes := []models.Node{}

	for _, c := range dynamoNodes {
		// Determine the effective status
		nodeStatus := c.Status
		if c.Status != "Pending" {
			// Check if account is paused
			if accountStatus, exists := accountStatusMap[c.AccountUuid]; exists && accountStatus == "Paused" {
				nodeStatus = "Paused"
			}
		}
		
		// Get account owner if available
		accountOwnerId := ""
		if accountOwnerMap != nil {
			accountOwnerId = accountOwnerMap[c.AccountUuid]
		}
		
		// Convert INDEPENDENT back to empty string for API response consistency
		responseOrganizationId := c.OrganizationId
		if c.OrganizationId == "INDEPENDENT" {
			responseOrganizationId = ""
		}
		
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
			OrganizationId:     responseOrganizationId,
			NodeOwnerId:        c.UserId,
			AccountOwnerId:     accountOwnerId,
			Identifier:         c.Identifier,
			WorkflowManagerTag: c.WorkflowManagerTag,
			Status:             nodeStatus,
		})
	}

	return nodes
}

func DynamoDBNodeToJsonNode(dynamoNodes []models.DynamoDBNode) []models.Node {
	nodes := []models.Node{}

	for _, c := range dynamoNodes {
		// Convert INDEPENDENT back to empty string for API response consistency
		responseOrganizationId := c.OrganizationId
		if c.OrganizationId == "INDEPENDENT" {
			responseOrganizationId = ""
		}
		
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
			OrganizationId:     responseOrganizationId,
			NodeOwnerId:        c.UserId,
			Identifier:         c.Identifier,
			WorkflowManagerTag: c.WorkflowManagerTag,
			Status:             c.Status,
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