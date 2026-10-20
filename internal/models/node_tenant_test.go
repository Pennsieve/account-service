package models

import (
	"testing"

	"github.com/pennsieve/pennsieve-go-core/pkg/models/tenant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	orgNodeId  = "N:organization:3f2504e0-4f89-41d3-9a0c-0305e82c3301"
	userNodeId = "N:user:9b2c1f7e-8d4a-4e6b-b1c3-2a5f6e7d8c90"
)

func TestNodeTenant(t *testing.T) {
	org, err := NodeTenant(orgNodeId, userNodeId)
	require.NoError(t, err)
	assert.Equal(t, tenant.OrganizationKind, org.Kind(), "a node in an organization belongs to it")
	assert.Equal(t, orgNodeId, org.NodeId())

	user, err := NodeTenant("", userNodeId)
	require.NoError(t, err)
	assert.Equal(t, tenant.UserKind, user.Kind(), "a node with no organization belongs to its creator")
	assert.Equal(t, userNodeId, user.NodeId())

	_, err = NodeTenant("org-123", userNodeId)
	assert.Error(t, err)
	_, err = NodeTenant("", "user-123")
	assert.Error(t, err)
	_, err = NodeTenant("INDEPENDENT", userNodeId)
	assert.Error(t, err, "the legacy value is not an organization")
}

func TestLegacyOrganizationId(t *testing.T) {
	org, _ := tenant.FromOrgNodeId(orgNodeId)
	user, _ := tenant.FromUserNodeId(userNodeId)
	assert.Equal(t, orgNodeId, LegacyOrganizationId(org))
	assert.Equal(t, "INDEPENDENT", LegacyOrganizationId(user), "workflow-service and the provisioner still read this value")
}

func TestOrganizationFromLegacy(t *testing.T) {
	assert.Equal(t, "", OrganizationFromLegacy("INDEPENDENT"))
	assert.Equal(t, "", OrganizationFromLegacy(""))
	assert.Equal(t, orgNodeId, OrganizationFromLegacy(orgNodeId))
}

func TestWithTenantFillsARecordFromBeforeTenants(t *testing.T) {
	orgNode, err := DynamoDBNode{Uuid: "n1", OrganizationId: orgNodeId, UserId: userNodeId}.WithTenant()
	require.NoError(t, err)
	assert.Equal(t, "ORG#3f2504e0-4f89-41d3-9a0c-0305e82c3301", orgNode.Tenant.String())

	userNode, err := DynamoDBNode{Uuid: "n2", OrganizationId: "INDEPENDENT", UserId: userNodeId}.WithTenant()
	require.NoError(t, err)
	assert.Equal(t, "USER#9b2c1f7e-8d4a-4e6b-b1c3-2a5f6e7d8c90", userNode.Tenant.String())

	_, err = DynamoDBNode{Uuid: "n3", OrganizationId: "INDEPENDENT", UserId: "user-123"}.WithTenant()
	assert.Error(t, err)
}

func TestWithTenantKeepsATenant(t *testing.T) {
	user, _ := tenant.FromUserNodeId(userNodeId)
	node, err := DynamoDBNode{Uuid: "n1", Tenant: user, OrganizationId: orgNodeId}.WithTenant()
	require.NoError(t, err)
	assert.Equal(t, user, node.Tenant, "the tenant wins over organizationId")
}

func TestOrganizationNodeId(t *testing.T) {
	org, _ := tenant.FromOrgNodeId(orgNodeId)
	user, _ := tenant.FromUserNodeId(userNodeId)

	assert.Equal(t, orgNodeId, DynamoDBNode{Tenant: org}.OrganizationNodeId())
	assert.Equal(t, "", DynamoDBNode{Tenant: user, OrganizationId: "INDEPENDENT"}.OrganizationNodeId())

	// A record without a tenant falls back to organizationId.
	assert.Equal(t, orgNodeId, DynamoDBNode{OrganizationId: orgNodeId}.OrganizationNodeId())
	assert.Equal(t, "", DynamoDBNode{OrganizationId: "INDEPENDENT"}.OrganizationNodeId())
	assert.Equal(t, "", DynamoDBNode{}.OrganizationNodeId())
}
