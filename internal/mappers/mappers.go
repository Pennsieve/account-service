package mappers

import (
	"os"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
)

// DynamoDBNodeToJsonNodeWithAccountStatus converts DynamoDB nodes to JSON nodes with account status override
// If the account is Paused and the node is not Pending, the node status will be overridden to Paused
func DynamoDBNodeToJsonNodeWithAccountStatus(dynamoNodes []models.DynamoDBNode, accountStatusMap map[string]string) []models.Node {
	return DynamoDBNodeToJsonNodeWithAccountInfo(dynamoNodes, accountStatusMap, nil, nil)
}

// DynamoDBNodeToJsonNodeWithAccountInfo converts DynamoDB nodes to JSON nodes with account status override and owner info
// If the account is Paused and the node is not Pending, the node status will be overridden to Paused
// Also includes the account owner's userId if available
func DynamoDBNodeToJsonNodeWithAccountInfo(dynamoNodes []models.DynamoDBNode, accountStatusMap map[string]string, accountOwnerMap map[string]string, accessScopeMap map[string]models.NodeAccessScope) []models.Node {
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

		// Get access scope if available
		var accessScope models.NodeAccessScope
		if accessScopeMap != nil {
			accessScope = accessScopeMap[c.Uuid]
		}

		responseOrganizationId := c.OrganizationNodeId()

		nodes = append(nodes, models.Node{
			Uuid:        c.Uuid,
			Name:        c.Name,
			Description: c.Description,
			QueueUrl:    c.QueueUrl,
			Account: models.NodeAccount{
				Uuid:        c.AccountUuid,
				AccountId:   c.AccountId,
				AccountType: c.AccountType,
				OwnerId:     accountOwnerId,
			},
			CreatedAt:           c.CreatedAt,
			OrganizationId:      responseOrganizationId,
			OwnerId:             c.UserId,
			Identifier:          c.Identifier,
			WorkflowManagerTag:  c.WorkflowManagerTag,
			ProvisionerImage:    c.ProvisionerImage,
			ProvisionerImageTag: c.ProvisionerImageTag,
			PendingProvisionerImageTag: c.PendingProvisionerImageTag,
			DeploymentMode:      c.DeploymentMode,
			EnableLLMAccess:     c.EnableLLMAccess,
			LlmBaaAcknowledged:  c.LlmBaaAcknowledged,
			MaxGpuInstances:        c.MaxGpuInstances,
			GpuTier:                c.GpuTier,
			MaxInteractiveSessions: c.MaxInteractiveSessions,
			AccessScope:            accessScope,
			Status:                 nodeStatus,
			HealthStatus:           c.HealthStatus,
			LastHealthCheck:        c.LastHealthCheck,
		})
	}

	return nodes
}

func DynamoDBNodeToJsonNode(dynamoNodes []models.DynamoDBNode) []models.Node {
	nodes := []models.Node{}

	for _, c := range dynamoNodes {
		responseOrganizationId := c.OrganizationNodeId()
		
		nodes = append(nodes, models.Node{
			Uuid:        c.Uuid,
			Name:        c.Name,
			Description: c.Description,
			QueueUrl:    c.QueueUrl,
			Account: models.NodeAccount{
				Uuid:        c.AccountUuid,
				AccountId:   c.AccountId,
				AccountType: c.AccountType,
			},
			CreatedAt:           c.CreatedAt,
			OrganizationId:      responseOrganizationId,
			OwnerId:             c.UserId,
			Identifier:          c.Identifier,
			WorkflowManagerTag:  c.WorkflowManagerTag,
			ProvisionerImage:    c.ProvisionerImage,
			ProvisionerImageTag: c.ProvisionerImageTag,
			PendingProvisionerImageTag: c.PendingProvisionerImageTag,
			DeploymentMode:      c.DeploymentMode,
			EnableLLMAccess:     c.EnableLLMAccess,
			LlmBaaAcknowledged:  c.LlmBaaAcknowledged,
			MaxGpuInstances:        c.MaxGpuInstances,
			GpuTier:                c.GpuTier,
			MaxInteractiveSessions: c.MaxInteractiveSessions,
			Status:                 c.Status,
			HealthStatus:           c.HealthStatus,
			LastHealthCheck:        c.LastHealthCheck,
		})
	}

	return nodes
}

func DynamoDBAccountToJsonAccount(dynamoAccounts []store_dynamodb.Account, callerId string) []models.Account {
	accounts := []models.Account{}

	for _, c := range dynamoAccounts {
		accounts = append(accounts, WithConnection(models.Account{
			Uuid:        c.Uuid,
			AccountId:   c.AccountId,
			AccountType: c.AccountType,
			RoleName:    c.RoleName,
			RuntimeRoleName: c.RuntimeRoleName,
			ExternalId:  c.ExternalId,
			UserId:      c.UserId,
			Name:        c.Name,
			Description: c.Description,
			Status:      c.Status,
		}, c, callerId))
	}

	return accounts
}

// WithConnection adds how the account's role was made, and the published
// template's version, for the account's owner (callerId) only, as with its
// role name and external ID.
func WithConnection(m models.Account, a store_dynamodb.Account, callerId string) models.Account {
	if a.UserId == "" || a.UserId != callerId {
		return m
	}
	m.Connection = a.Connection
	if m.Connection == "" {
		m.Connection = models.ConnectionLegacy
	}
	m.TemplateVersion = a.TemplateVersion
	m.LatestTemplateVersion = os.Getenv("CFN_TEMPLATE_VERSION")
	return m
}