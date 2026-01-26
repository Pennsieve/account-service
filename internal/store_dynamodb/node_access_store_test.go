package store_dynamodb

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getDynamoDBEndpoint() string {
	if endpoint := os.Getenv("DYNAMODB_URL"); endpoint != "" {
		return endpoint
	}
	return "http://localhost:8000"
}

func setupNodeAccessTest(t *testing.T, tableName string) (*dynamodb.Client, NodeAccessStore) {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     "test",
				SecretAccessKey: "test",
			}, nil
		})),
		config.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc(
			func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{URL: getDynamoDBEndpoint()}, nil
			})))
	require.NoError(t, err)

	client := dynamodb.NewFromConfig(cfg)
	
	// Create table
	_, err = createNodeAccessTable(client, tableName)
	require.NoError(t, err)
	
	// Register cleanup
	t.Cleanup(func() {
		_ = deleteNodeAccessTable(client, tableName)
	})
	
	store := NewNodeAccessDatabaseStore(client, tableName)
	return client, store
}

func TestNodeAccessStore_GrantAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	_, store := setupNodeAccessTest(t, "test-node-access-table-grant")

	// Test data
	nodeUuid := "test-node-123"
	userId := "user-456"
	organizationId := "org-789"

	access := models.NodeAccess{
		EntityId:       models.FormatEntityId(models.EntityTypeUser, userId),
		NodeId:         models.FormatNodeId(nodeUuid),
		EntityType:     models.EntityTypeUser,
		EntityRawId:    userId,
		NodeUuid:       nodeUuid,
		AccessType:     models.AccessTypeOwner,
		OrganizationId: organizationId,
		GrantedBy:      userId,
	}

	// Grant access
	err := store.GrantAccess(context.Background(), access)
	assert.NoError(t, err)

	// Verify access was granted
	hasAccess, err := store.HasAccess(context.Background(), access.EntityId, access.NodeId)
	assert.NoError(t, err)
	assert.True(t, hasAccess)

	// Verify granted timestamp was set
	accessList, err := store.GetNodeAccess(context.Background(), nodeUuid)
	assert.NoError(t, err)
	assert.Len(t, accessList, 1)
	assert.False(t, accessList[0].GrantedAt.IsZero())

	// Cleanup
	err = store.RevokeAccess(context.Background(), access.EntityId, access.NodeId)
	assert.NoError(t, err)
}

func TestNodeAccessStore_RevokeAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	_, store := setupNodeAccessTest(t, "test-node-access-table-revoke")

	// Test data
	nodeUuid := "test-node-456"
	userId := "user-789"
	organizationId := "org-123"

	access := models.NodeAccess{
		EntityId:       models.FormatEntityId(models.EntityTypeUser, userId),
		NodeId:         models.FormatNodeId(nodeUuid),
		EntityType:     models.EntityTypeUser,
		EntityRawId:    userId,
		NodeUuid:       nodeUuid,
		AccessType:     models.AccessTypeShared,
		OrganizationId: organizationId,
		GrantedBy:      "admin-user",
	}

	// First grant access
	err := store.GrantAccess(context.Background(), access)
	require.NoError(t, err)

	// Verify access exists
	hasAccess, err := store.HasAccess(context.Background(), access.EntityId, access.NodeId)
	assert.NoError(t, err)
	assert.True(t, hasAccess)

	// Revoke access
	err = store.RevokeAccess(context.Background(), access.EntityId, access.NodeId)
	assert.NoError(t, err)

	// Verify access was revoked
	hasAccess, err = store.HasAccess(context.Background(), access.EntityId, access.NodeId)
	assert.NoError(t, err)
	assert.False(t, hasAccess)
}

func TestNodeAccessStore_HasAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	_, store := setupNodeAccessTest(t, "test-node-access-table-hasaccess")

	// Test data
	nodeUuid := "test-node-789"
	userId := "user-123"
	entityId := models.FormatEntityId(models.EntityTypeUser, userId)
	nodeId := models.FormatNodeId(nodeUuid)

	// Check non-existent access
	hasAccess, err := store.HasAccess(context.Background(), entityId, nodeId)
	assert.NoError(t, err)
	assert.False(t, hasAccess)

	// Grant access
	access := models.NodeAccess{
		EntityId:       entityId,
		NodeId:         nodeId,
		EntityType:     models.EntityTypeUser,
		EntityRawId:    userId,
		NodeUuid:       nodeUuid,
		AccessType:     models.AccessTypeShared,
		OrganizationId: "org-456",
		GrantedBy:      "admin-user",
	}

	err = store.GrantAccess(context.Background(), access)
	require.NoError(t, err)

	// Check existing access
	hasAccess, err = store.HasAccess(context.Background(), entityId, nodeId)
	assert.NoError(t, err)
	assert.True(t, hasAccess)

	// Cleanup
	err = store.RevokeAccess(context.Background(), entityId, nodeId)
	assert.NoError(t, err)
}

func TestNodeAccessStore_GetNodeAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	_, store := setupNodeAccessTest(t, "test-node-access-table-getnodeaccess")

	// Test data
	nodeUuid := "test-node-multi-access"
	organizationId := "org-test"

	// Create multiple access entries for the same node
	accesses := []models.NodeAccess{
		{
			EntityId:       models.FormatEntityId(models.EntityTypeUser, "owner-123"),
			NodeId:         models.FormatNodeId(nodeUuid),
			EntityType:     models.EntityTypeUser,
			EntityRawId:    "owner-123",
			NodeUuid:       nodeUuid,
			AccessType:     models.AccessTypeOwner,
			OrganizationId: organizationId,
			GrantedBy:      "owner-123",
		},
		{
			EntityId:       models.FormatEntityId(models.EntityTypeUser, "user-456"),
			NodeId:         models.FormatNodeId(nodeUuid),
			EntityType:     models.EntityTypeUser,
			EntityRawId:    "user-456",
			NodeUuid:       nodeUuid,
			AccessType:     models.AccessTypeShared,
			OrganizationId: organizationId,
			GrantedBy:      "owner-123",
		},
		{
			EntityId:       models.FormatEntityId(models.EntityTypeTeam, "team-789"),
			NodeId:         models.FormatNodeId(nodeUuid),
			EntityType:     models.EntityTypeTeam,
			EntityRawId:    "team-789",
			NodeUuid:       nodeUuid,
			AccessType:     models.AccessTypeShared,
			OrganizationId: organizationId,
			GrantedBy:      "owner-123",
		},
	}

	// Grant all access entries
	for _, access := range accesses {
		err := store.GrantAccess(context.Background(), access)
		require.NoError(t, err)
	}

	// Get all access for the node
	accessList, err := store.GetNodeAccess(context.Background(), nodeUuid)
	assert.NoError(t, err)
	assert.Len(t, accessList, 3)

	// Verify all access types are present
	accessTypes := make(map[models.AccessType]bool)
	entityTypes := make(map[models.EntityType]bool)
	for _, access := range accessList {
		accessTypes[access.AccessType] = true
		entityTypes[access.EntityType] = true
		assert.Equal(t, nodeUuid, access.NodeUuid)
		assert.Equal(t, organizationId, access.OrganizationId)
	}

	assert.True(t, accessTypes[models.AccessTypeOwner])
	assert.True(t, accessTypes[models.AccessTypeShared])
	assert.True(t, entityTypes[models.EntityTypeUser])
	assert.True(t, entityTypes[models.EntityTypeTeam])

	// Cleanup
	for _, access := range accesses {
		err = store.RevokeAccess(context.Background(), access.EntityId, access.NodeId)
		assert.NoError(t, err)
	}
}

func TestNodeAccessStore_GetEntityAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	_, store := setupNodeAccessTest(t, "test-node-access-table-getentityaccess")

	// Test data
	userId := "user-entity-test"
	entityId := models.FormatEntityId(models.EntityTypeUser, userId)
	organizationId := "org-entity-test"

	// Create access to multiple nodes for the same user
	nodeUuids := []string{"node-1", "node-2", "node-3"}
	var accesses []models.NodeAccess

	for _, nodeUuid := range nodeUuids {
		access := models.NodeAccess{
			EntityId:       entityId,
			NodeId:         models.FormatNodeId(nodeUuid),
			EntityType:     models.EntityTypeUser,
			EntityRawId:    userId,
			NodeUuid:       nodeUuid,
			AccessType:     models.AccessTypeShared,
			OrganizationId: organizationId,
			GrantedBy:      "admin-user",
		}
		accesses = append(accesses, access)

		err := store.GrantAccess(context.Background(), access)
		require.NoError(t, err)
	}

	// Get all access for the entity
	accessList, err := store.GetEntityAccess(context.Background(), entityId)
	assert.NoError(t, err)
	assert.Len(t, accessList, 3)

	// Verify all nodes are accessible
	accessibleNodes := make(map[string]bool)
	for _, access := range accessList {
		accessibleNodes[access.NodeUuid] = true
		assert.Equal(t, entityId, access.EntityId)
		assert.Equal(t, userId, access.EntityRawId)
		assert.Equal(t, models.EntityTypeUser, access.EntityType)
		assert.Equal(t, models.AccessTypeShared, access.AccessType)
	}

	for _, nodeUuid := range nodeUuids {
		assert.True(t, accessibleNodes[nodeUuid], "Node %s should be accessible", nodeUuid)
	}

	// Cleanup
	for _, access := range accesses {
		err = store.RevokeAccess(context.Background(), access.EntityId, access.NodeId)
		assert.NoError(t, err)
	}
}

func TestNodeAccessStore_GetWorkspaceNodes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	_, store := setupNodeAccessTest(t, "test-node-access-table-getworkspacenodes")

	// Test data
	organizationId := "org-workspace-test"
	workspaceEntityId := models.FormatEntityId(models.EntityTypeWorkspace, organizationId)

	// Create workspace access and some regular access
	accesses := []models.NodeAccess{
		// Workspace access (should be returned)
		{
			EntityId:       workspaceEntityId,
			NodeId:         models.FormatNodeId("workspace-node-1"),
			EntityType:     models.EntityTypeWorkspace,
			EntityRawId:    organizationId,
			NodeUuid:       "workspace-node-1",
			AccessType:     models.AccessTypeWorkspace,
			OrganizationId: organizationId,
			GrantedBy:      "admin-user",
		},
		{
			EntityId:       workspaceEntityId,
			NodeId:         models.FormatNodeId("workspace-node-2"),
			EntityType:     models.EntityTypeWorkspace,
			EntityRawId:    organizationId,
			NodeUuid:       "workspace-node-2",
			AccessType:     models.AccessTypeWorkspace,
			OrganizationId: organizationId,
			GrantedBy:      "admin-user",
		},
		// User access (should not be returned)
		{
			EntityId:       models.FormatEntityId(models.EntityTypeUser, "user-123"),
			NodeId:         models.FormatNodeId("user-node-1"),
			EntityType:     models.EntityTypeUser,
			EntityRawId:    "user-123",
			NodeUuid:       "user-node-1",
			AccessType:     models.AccessTypeShared,
			OrganizationId: organizationId,
			GrantedBy:      "admin-user",
		},
	}

	// Grant all access entries
	for _, access := range accesses {
		err := store.GrantAccess(context.Background(), access)
		require.NoError(t, err)
	}

	// Get workspace nodes
	workspaceNodes, err := store.GetWorkspaceNodes(context.Background(), organizationId)
	assert.NoError(t, err)
	assert.Len(t, workspaceNodes, 2) // Only workspace access should be returned

	// Verify all returned nodes have workspace access type
	for _, access := range workspaceNodes {
		assert.Equal(t, models.AccessTypeWorkspace, access.AccessType)
		assert.Equal(t, models.EntityTypeWorkspace, access.EntityType)
		assert.Equal(t, organizationId, access.OrganizationId)
		assert.Equal(t, organizationId, access.EntityRawId)
	}

	// Cleanup
	for _, access := range accesses {
		err = store.RevokeAccess(context.Background(), access.EntityId, access.NodeId)
		assert.NoError(t, err)
	}
}

func TestNodeAccessStore_BatchCheckAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	_, store := setupNodeAccessTest(t, "test-node-access-table-batchcheck")

	// Test data
	nodeUuid := "batch-check-node"
	nodeId := models.FormatNodeId(nodeUuid)
	organizationId := "org-batch-test"

	// Grant access to only one entity
	grantedEntityId := models.FormatEntityId(models.EntityTypeUser, "user-with-access")
	access := models.NodeAccess{
		EntityId:       grantedEntityId,
		NodeId:         nodeId,
		EntityType:     models.EntityTypeUser,
		EntityRawId:    "user-with-access",
		NodeUuid:       nodeUuid,
		AccessType:     models.AccessTypeShared,
		OrganizationId: organizationId,
		GrantedBy:      "admin-user",
	}

	err := store.GrantAccess(context.Background(), access)
	require.NoError(t, err)

	// Test batch check with mix of entities (some with access, some without)
	entityIds := []string{
		models.FormatEntityId(models.EntityTypeUser, "user-without-access-1"),
		grantedEntityId, // This one has access
		models.FormatEntityId(models.EntityTypeUser, "user-without-access-2"),
		models.FormatEntityId(models.EntityTypeTeam, "team-without-access"),
	}

	// Should return true because at least one entity has access
	hasAccess, err := store.BatchCheckAccess(context.Background(), entityIds, nodeId)
	assert.NoError(t, err)
	assert.True(t, hasAccess)

	// Test with entities that don't have access
	noAccessEntityIds := []string{
		models.FormatEntityId(models.EntityTypeUser, "user-no-access-1"),
		models.FormatEntityId(models.EntityTypeUser, "user-no-access-2"),
	}

	hasAccess, err = store.BatchCheckAccess(context.Background(), noAccessEntityIds, nodeId)
	assert.NoError(t, err)
	assert.False(t, hasAccess)

	// Cleanup
	err = store.RevokeAccess(context.Background(), grantedEntityId, nodeId)
	assert.NoError(t, err)
}

func TestNodeAccessStore_UpdateNodeAccessScope(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	_, store := setupNodeAccessTest(t, "test-node-access-table-updatescope")

	// Test data
	nodeUuid := "access-scope-test-node"
	organizationId := "org-scope-test"
	grantedBy := "admin-user"

	// Test setting to workspace scope
	err := store.UpdateNodeAccessScope(context.Background(), nodeUuid, models.AccessScopeWorkspace, organizationId, grantedBy)
	assert.NoError(t, err)

	// Verify workspace access was granted
	workspaceEntityId := models.FormatEntityId(models.EntityTypeWorkspace, organizationId)
	nodeId := models.FormatNodeId(nodeUuid)
	hasAccess, err := store.HasAccess(context.Background(), workspaceEntityId, nodeId)
	assert.NoError(t, err)
	assert.True(t, hasAccess)

	// Test setting to private scope (should remove workspace access)
	err = store.UpdateNodeAccessScope(context.Background(), nodeUuid, models.AccessScopePrivate, organizationId, grantedBy)
	assert.NoError(t, err)

	// Verify workspace access was removed
	hasAccess, err = store.HasAccess(context.Background(), workspaceEntityId, nodeId)
	assert.NoError(t, err)
	assert.False(t, hasAccess)

	// Test setting back to workspace scope
	err = store.UpdateNodeAccessScope(context.Background(), nodeUuid, models.AccessScopeWorkspace, organizationId, grantedBy)
	assert.NoError(t, err)

	// Verify workspace access was re-granted
	hasAccess, err = store.HasAccess(context.Background(), workspaceEntityId, nodeId)
	assert.NoError(t, err)
	assert.True(t, hasAccess)

	// Cleanup
	err = store.RevokeAccess(context.Background(), workspaceEntityId, nodeId)
	assert.NoError(t, err)
}

func TestNodeAccessStore_BatchGrantAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	_, store := setupNodeAccessTest(t, "test-node-access-table-batchgrant")
	storeImpl := store.(*NodeAccessDatabaseStore)

	// Test data
	nodeUuid := "batch-grant-test-node"
	organizationId := "org-batch-grant-test"

	// Create multiple access entries to grant in batch
	accesses := []models.NodeAccess{
		{
			EntityId:       models.FormatEntityId(models.EntityTypeUser, "batch-user-1"),
			NodeId:         models.FormatNodeId(nodeUuid),
			EntityType:     models.EntityTypeUser,
			EntityRawId:    "batch-user-1",
			NodeUuid:       nodeUuid,
			AccessType:     models.AccessTypeShared,
			OrganizationId: organizationId,
			GrantedBy:      "admin-user",
		},
		{
			EntityId:       models.FormatEntityId(models.EntityTypeUser, "batch-user-2"),
			NodeId:         models.FormatNodeId(nodeUuid),
			EntityType:     models.EntityTypeUser,
			EntityRawId:    "batch-user-2",
			NodeUuid:       nodeUuid,
			AccessType:     models.AccessTypeShared,
			OrganizationId: organizationId,
			GrantedBy:      "admin-user",
		},
		{
			EntityId:       models.FormatEntityId(models.EntityTypeTeam, "batch-team-1"),
			NodeId:         models.FormatNodeId(nodeUuid),
			EntityType:     models.EntityTypeTeam,
			EntityRawId:    "batch-team-1",
			NodeUuid:       nodeUuid,
			AccessType:     models.AccessTypeShared,
			OrganizationId: organizationId,
			GrantedBy:      "admin-user",
		},
	}

	// Batch grant access
	err := storeImpl.BatchGrantAccess(context.Background(), accesses)
	assert.NoError(t, err)

	// Verify all access was granted
	for _, access := range accesses {
		hasAccess, err := store.HasAccess(context.Background(), access.EntityId, access.NodeId)
		assert.NoError(t, err)
		assert.True(t, hasAccess, "Access should be granted for %s", access.EntityId)
	}

	// Verify granted timestamps were set
	accessList, err := store.GetNodeAccess(context.Background(), nodeUuid)
	assert.NoError(t, err)
	assert.Len(t, accessList, 3)

	for _, access := range accessList {
		assert.False(t, access.GrantedAt.IsZero(), "GrantedAt should be set for %s", access.EntityId)
		assert.WithinDuration(t, time.Now(), access.GrantedAt, 10*time.Second)
	}

	// Cleanup
	for _, access := range accesses {
		err = store.RevokeAccess(context.Background(), access.EntityId, access.NodeId)
		assert.NoError(t, err)
	}
}

func TestNodeAccessStore_RemoveAllNodeAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	_, store := setupNodeAccessTest(t, "test-node-access-table-removeall")

	// Test data
	nodeUuid := "remove-all-test-node"
	organizationId := "org-remove-all-test"

	// Create multiple access entries for the same node
	accesses := []models.NodeAccess{
		{
			EntityId:       models.FormatEntityId(models.EntityTypeUser, "remove-user-1"),
			NodeId:         models.FormatNodeId(nodeUuid),
			EntityType:     models.EntityTypeUser,
			EntityRawId:    "remove-user-1",
			NodeUuid:       nodeUuid,
			AccessType:     models.AccessTypeOwner,
			OrganizationId: organizationId,
			GrantedBy:      "remove-user-1",
		},
		{
			EntityId:       models.FormatEntityId(models.EntityTypeUser, "remove-user-2"),
			NodeId:         models.FormatNodeId(nodeUuid),
			EntityType:     models.EntityTypeUser,
			EntityRawId:    "remove-user-2",
			NodeUuid:       nodeUuid,
			AccessType:     models.AccessTypeShared,
			OrganizationId: organizationId,
			GrantedBy:      "remove-user-1",
		},
		{
			EntityId:       models.FormatEntityId(models.EntityTypeWorkspace, organizationId),
			NodeId:         models.FormatNodeId(nodeUuid),
			EntityType:     models.EntityTypeWorkspace,
			EntityRawId:    organizationId,
			NodeUuid:       nodeUuid,
			AccessType:     models.AccessTypeWorkspace,
			OrganizationId: organizationId,
			GrantedBy:      "remove-user-1",
		},
	}

	// Grant all access entries
	for _, access := range accesses {
		err := store.GrantAccess(context.Background(), access)
		require.NoError(t, err)
	}

	// Verify all access exists
	accessList, err := store.GetNodeAccess(context.Background(), nodeUuid)
	assert.NoError(t, err)
	assert.Len(t, accessList, 3)

	// Remove all access
	err = store.RemoveAllNodeAccess(context.Background(), nodeUuid)
	assert.NoError(t, err)

	// Verify all access was removed
	accessList, err = store.GetNodeAccess(context.Background(), nodeUuid)
	assert.NoError(t, err)
	assert.Len(t, accessList, 0)

	// Verify individual access checks return false
	for _, access := range accesses {
		hasAccess, err := store.HasAccess(context.Background(), access.EntityId, access.NodeId)
		assert.NoError(t, err)
		assert.False(t, hasAccess, "Access should be removed for %s", access.EntityId)
	}
}

func createNodeAccessTable(client *dynamodb.Client, tableName string) (*types.TableDescription, error) {
	table, err := client.CreateTable(context.TODO(), &dynamodb.CreateTableInput{
		AttributeDefinitions: []types.AttributeDefinition{
			{
				AttributeName: aws.String("entityId"),
				AttributeType: types.ScalarAttributeTypeS,
			},
			{
				AttributeName: aws.String("nodeId"),
				AttributeType: types.ScalarAttributeTypeS,
			},
			{
				AttributeName: aws.String("organizationId"),
				AttributeType: types.ScalarAttributeTypeS,
			},
		},
		KeySchema: []types.KeySchemaElement{
			{
				AttributeName: aws.String("entityId"),
				KeyType:       types.KeyTypeHash,
			},
			{
				AttributeName: aws.String("nodeId"),
				KeyType:       types.KeyTypeRange,
			},
		},
		GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
			{
				IndexName: aws.String("nodeId-entityId-index"),
				KeySchema: []types.KeySchemaElement{
					{
						AttributeName: aws.String("nodeId"),
						KeyType:       types.KeyTypeHash,
					},
					{
						AttributeName: aws.String("entityId"),
						KeyType:       types.KeyTypeRange,
					},
				},
				Projection: &types.Projection{
					ProjectionType: types.ProjectionTypeAll,
				},
			},
			{
				IndexName: aws.String("organizationId-nodeId-index"),
				KeySchema: []types.KeySchemaElement{
					{
						AttributeName: aws.String("organizationId"),
						KeyType:       types.KeyTypeHash,
					},
					{
						AttributeName: aws.String("nodeId"),
						KeyType:       types.KeyTypeRange,
					},
				},
				Projection: &types.Projection{
					ProjectionType: types.ProjectionTypeAll,
				},
			},
		},
		TableName:   aws.String(tableName),
		BillingMode: types.BillingModePayPerRequest,
	})
	
	if err != nil {
		log.Printf("couldn't create table %v. Here's why: %v\n", tableName, err)
		return nil, err
	}
	
	waiter := dynamodb.NewTableExistsWaiter(client)
	err = waiter.Wait(context.TODO(), &dynamodb.DescribeTableInput{
		TableName: aws.String(tableName)}, 2*time.Minute)
	if err != nil {
		log.Printf("wait for table exists failed. Here's why: %v\n", err)
	}
	
	return table.TableDescription, err
}

func deleteNodeAccessTable(client *dynamodb.Client, tableName string) error {
	_, err := client.DeleteTable(context.TODO(), &dynamodb.DeleteTableInput{
		TableName: aws.String(tableName)})
	if err != nil {
		log.Printf("couldn't delete table %v. Here's why: %v\n", tableName, err)
	}
	return err
}