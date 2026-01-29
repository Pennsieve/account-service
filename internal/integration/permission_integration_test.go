package integration

import (
    "context"
    "log"
    "os"
    "testing"
    "time"

    "github.com/pennsieve/account-service/internal/models"
    "github.com/pennsieve/account-service/internal/service"
    "github.com/pennsieve/account-service/internal/store_dynamodb"
    "github.com/pennsieve/account-service/internal/test"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// Integration tests for permission workflows
// These tests validate the complete permission system end-to-end

// TestMain sets up tables once for the entire integration package
func TestMain(m *testing.M) {
    // Setup: Create client and tables
    _ = test.GetClient() // Initialize the global client
    
    if err := test.SetupPackageTables(); err != nil {
        log.Fatalf("Failed to setup package tables: %v", err)
    }

    // Run all tests - individual tests clean up their own data with unique IDs
    exitCode := m.Run()

    os.Exit(exitCode)
}

func setupPermissionIntegrationTest(t *testing.T) (*service.PermissionService, *store_dynamodb.NodeAccessDatabaseStore, string) {
    if testing.Short() {
        t.Skip("Skipping integration test")
    }

    // Use the shared test client and access table
    client := test.GetClient()
    tableName := test.TEST_ACCESS_TABLE

    // Generate a unique test ID for this test run
    testId := test.GenerateTestId()

    // Don't clear the entire table - tests run in parallel!
    // Each test uses unique IDs to avoid conflicts

    // Register cleanup for this specific test's data
    t.Cleanup(func() {
        // Could implement targeted cleanup here if needed
        // For now, rely on unique IDs to prevent conflicts
    })

    nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(client, tableName)
    permissionService := service.NewPermissionService(nodeAccessStore, nil) // No team store for simplicity

    return permissionService, nodeAccessStore.(*store_dynamodb.NodeAccessDatabaseStore), testId
}

func TestPermissionWorkflow_PrivateToWorkspaceToShared(t *testing.T) {
    permissionService, nodeAccessStore, testId := setupPermissionIntegrationTest(t)

    ctx := context.Background()
    nodeUuid := "integration-test-node-1-" + testId
    ownerId := "owner-123-" + testId
    organizationId := "org-456-" + testId
    grantedBy := ownerId

    // Step 1: Create node with private access (owner only)
    req := models.NodeAccessRequest{
        NodeUuid:    nodeUuid,
        AccessScope: models.AccessScopePrivate,
    }

    err := permissionService.SetNodePermissions(ctx, nodeUuid, req, ownerId, organizationId, grantedBy)
    assert.NoError(t, err)

    // Verify private access
    permissions, err := permissionService.GetNodePermissions(ctx, nodeUuid)
    assert.NoError(t, err)
    assert.Equal(t, models.AccessScopePrivate, permissions.AccessScope)
    assert.Equal(t, ownerId, permissions.Owner)
    assert.Empty(t, permissions.SharedWithUsers)
    assert.Empty(t, permissions.SharedWithTeams)

    // Verify owner has access
    hasAccess, err := permissionService.CheckNodeAccess(ctx, ownerId, nodeUuid, organizationId)
    assert.NoError(t, err)
    assert.True(t, hasAccess)

    // Verify other users don't have access
    hasAccess, err = permissionService.CheckNodeAccess(ctx, "other-user", nodeUuid, organizationId)
    assert.NoError(t, err)
    assert.False(t, hasAccess)

    // Step 2: Change to workspace access
    req.AccessScope = models.AccessScopeWorkspace
    err = permissionService.SetNodePermissions(ctx, nodeUuid, req, ownerId, organizationId, grantedBy)
    assert.NoError(t, err)

    // Verify workspace access
    permissions, err = permissionService.GetNodePermissions(ctx, nodeUuid)
    assert.NoError(t, err)
    assert.Equal(t, models.AccessScopeWorkspace, permissions.AccessScope)
    assert.Equal(t, organizationId, permissions.OrganizationId)

    // Verify workspace users have access (simulated by checking workspace entity directly)
    workspaceEntityId := models.FormatEntityId(models.EntityTypeWorkspace, organizationId)
    nodeId := models.FormatNodeId(nodeUuid)
    hasAccess, err = nodeAccessStore.HasAccess(ctx, workspaceEntityId, nodeId)
    assert.NoError(t, err)
    assert.True(t, hasAccess)

    // Step 3: Change to shared access with specific users
    req.AccessScope = models.AccessScopeShared
    req.SharedWithUsers = []string{"shared-user-1", "shared-user-2"}
    req.SharedWithTeams = []string{"team-1"}

    err = permissionService.SetNodePermissions(ctx, nodeUuid, req, ownerId, organizationId, grantedBy)
    assert.NoError(t, err)

    // Verify shared access
    permissions, err = permissionService.GetNodePermissions(ctx, nodeUuid)
    assert.NoError(t, err)
    assert.Equal(t, models.AccessScopeShared, permissions.AccessScope)
    assert.Contains(t, permissions.SharedWithUsers, "shared-user-1")
    assert.Contains(t, permissions.SharedWithUsers, "shared-user-2")
    assert.Contains(t, permissions.SharedWithTeams, "team-1")

    // Verify workspace access was removed
    hasAccess, err = nodeAccessStore.HasAccess(ctx, workspaceEntityId, nodeId)
    assert.NoError(t, err)
    assert.False(t, hasAccess)

    // Verify shared users have access
    sharedUserEntityId := models.FormatEntityId(models.EntityTypeUser, "shared-user-1")
    hasAccess, err = nodeAccessStore.HasAccess(ctx, sharedUserEntityId, nodeId)
    assert.NoError(t, err)
    assert.True(t, hasAccess)

    // Clean up
    err = nodeAccessStore.RemoveAllNodeAccess(ctx, nodeUuid)
    assert.NoError(t, err)
}

func TestPermissionWorkflow_SharedAccessManagement(t *testing.T) {
    permissionService, nodeAccessStore, testId := setupPermissionIntegrationTest(t)

    ctx := context.Background()
    // Use unique IDs to avoid conflicts with parallel tests
    nodeUuid := "shared-mgmt-node-" + testId
    ownerId := "owner-" + testId
    organizationId := "org-" + testId
    grantedBy := ownerId

    // Step 1: Start with shared access to some users
    req := models.NodeAccessRequest{
        NodeUuid:        nodeUuid,
        AccessScope:     models.AccessScopeShared,
        SharedWithUsers: []string{"user-1", "user-2"},
        SharedWithTeams: []string{"team-1"},
    }

    err := permissionService.SetNodePermissions(ctx, nodeUuid, req, ownerId, organizationId, grantedBy)
    assert.NoError(t, err)

    // Verify initial shared access
    permissions, err := permissionService.GetNodePermissions(ctx, nodeUuid)
    assert.NoError(t, err)
    assert.Len(t, permissions.SharedWithUsers, 2)
    assert.Len(t, permissions.SharedWithTeams, 1)

    // Step 2: Modify shared access (add users, remove others)
    req.SharedWithUsers = []string{"user-1", "user-3"} // Remove user-2, add user-3
    req.SharedWithTeams = []string{"team-1", "team-2"} // Add team-2

    err = permissionService.SetNodePermissions(ctx, nodeUuid, req, ownerId, organizationId, grantedBy)
    assert.NoError(t, err)

    // Verify updated shared access
    permissions, err = permissionService.GetNodePermissions(ctx, nodeUuid)
    assert.NoError(t, err)
    assert.Contains(t, permissions.SharedWithUsers, "user-1")
    assert.Contains(t, permissions.SharedWithUsers, "user-3")
    assert.NotContains(t, permissions.SharedWithUsers, "user-2") // Should be removed
    assert.Contains(t, permissions.SharedWithTeams, "team-1")
    assert.Contains(t, permissions.SharedWithTeams, "team-2")

    // Verify access was actually revoked for user-2
    user2EntityId := models.FormatEntityId(models.EntityTypeUser, "user-2")
    nodeId := models.FormatNodeId(nodeUuid)
    hasAccess, err := nodeAccessStore.HasAccess(ctx, user2EntityId, nodeId)
    assert.NoError(t, err)
    assert.False(t, hasAccess)

    // Verify access was granted for user-3
    user3EntityId := models.FormatEntityId(models.EntityTypeUser, "user-3")
    hasAccess, err = nodeAccessStore.HasAccess(ctx, user3EntityId, nodeId)
    assert.NoError(t, err)
    assert.True(t, hasAccess)

    // Step 3: Remove all shared access (change to private)
    req.AccessScope = models.AccessScopePrivate
    req.SharedWithUsers = []string{}
    req.SharedWithTeams = []string{}

    err = permissionService.SetNodePermissions(ctx, nodeUuid, req, ownerId, organizationId, grantedBy)
    assert.NoError(t, err)

    // Verify all shared access was removed
    permissions, err = permissionService.GetNodePermissions(ctx, nodeUuid)
    assert.NoError(t, err)
    assert.Equal(t, models.AccessScopePrivate, permissions.AccessScope)
    assert.Empty(t, permissions.SharedWithUsers)
    assert.Empty(t, permissions.SharedWithTeams)

    // Verify no shared users have access
    user1EntityId := models.FormatEntityId(models.EntityTypeUser, "user-1")
    hasAccess, err = nodeAccessStore.HasAccess(ctx, user1EntityId, nodeId)
    assert.NoError(t, err)
    assert.False(t, hasAccess)

    // But owner still has access
    ownerEntityId := models.FormatEntityId(models.EntityTypeUser, ownerId)
    hasAccess, err = nodeAccessStore.HasAccess(ctx, ownerEntityId, nodeId)
    assert.NoError(t, err)
    assert.True(t, hasAccess)

    // Clean up
    err = nodeAccessStore.RemoveAllNodeAccess(ctx, nodeUuid)
    assert.NoError(t, err)
}

func TestPermissionWorkflow_AccessibleNodesQuery(t *testing.T) {
    permissionService, nodeAccessStore, testId := setupPermissionIntegrationTest(t)

    ctx := context.Background()
    // Use unique IDs to avoid conflicts with parallel tests
    userId := "test-user-" + testId
    organizationId := "org-" + testId

    // Create multiple nodes with different access patterns
    nodeConfigs := []struct {
        nodeUuid    string
        ownerId     string
        accessScope models.NodeAccessScope
        sharedUsers []string
    }{
        {
            nodeUuid:    "user-owned-node-" + testId,
            ownerId:     userId,
            accessScope: models.AccessScopePrivate,
        },
        {
            nodeUuid:    "user-shared-node-" + testId,
            ownerId:     "other-owner-" + testId,
            accessScope: models.AccessScopeShared,
            sharedUsers: []string{userId},
        },
        {
            nodeUuid:    "workspace-node-1-" + testId,
            ownerId:     "other-owner-2-" + testId,
            accessScope: models.AccessScopeWorkspace,
        },
        {
            nodeUuid:    "workspace-node-2-" + testId,
            ownerId:     "other-owner-3-" + testId,
            accessScope: models.AccessScopeWorkspace,
        },
        {
            nodeUuid:    "no-access-node-" + testId,
            ownerId:     "other-owner-4-" + testId,
            accessScope: models.AccessScopePrivate,
        },
    }

    // Set up all nodes
    for _, config := range nodeConfigs {
        req := models.NodeAccessRequest{
            NodeUuid:        config.nodeUuid,
            AccessScope:     config.accessScope,
            SharedWithUsers: config.sharedUsers,
        }

        err := permissionService.SetNodePermissions(ctx, config.nodeUuid, req, config.ownerId, organizationId, config.ownerId)
        require.NoError(t, err)
    }

    // Query accessible nodes with retry for GSI eventual consistency
    var accessibleNodes []string
    var err error
    
    // Retry mechanism for DynamoDB GSI eventual consistency under concurrent load
    maxRetries := 5
    for attempt := 0; attempt < maxRetries; attempt++ {
        accessibleNodes, err = permissionService.GetAccessibleNodes(ctx, userId, organizationId)
        assert.NoError(t, err)
        
        // If we get the expected number of nodes, break out of retry loop
        if len(accessibleNodes) == 4 {
            break
        }
        
        // Short delay before retry to allow GSI to catch up
        if attempt < maxRetries-1 {
            time.Sleep(50 * time.Millisecond)
        }
    }
    
    // Log if we still don't have the expected results after retries
    if len(accessibleNodes) != 4 {
        t.Logf("INFO: After %d attempts, got %d nodes instead of 4: %v", maxRetries, len(accessibleNodes), accessibleNodes)
        t.Logf("INFO: This may be due to DynamoDB GSI eventual consistency under concurrent test load")
    }

    // With unique organization IDs, we should only get nodes from this test
    // Convert to map for easier checking
    nodeMap := make(map[string]bool)
    for _, nodeUuid := range accessibleNodes {
        nodeMap[nodeUuid] = true
    }

    // Should have access to:
    // 1. user-owned-node (owned by user)
    // 2. user-shared-node (shared with user)
    // 3. workspace-node-1 (workspace access)
    // 4. workspace-node-2 (workspace access)
    // Should NOT have access to:
    // 5. no-access-node (private, not owned)

    assert.True(t, nodeMap["user-owned-node-" + testId], "Should have access to owned node")
    assert.True(t, nodeMap["user-shared-node-" + testId], "Should have access to shared node")
    assert.True(t, nodeMap["workspace-node-1-" + testId], "Should have access to workspace node 1")
    assert.True(t, nodeMap["workspace-node-2-" + testId], "Should have access to workspace node 2")
    assert.False(t, nodeMap["no-access-node-" + testId], "Should NOT have access to private node")

    assert.Len(t, accessibleNodes, 4, "Should have access to exactly 4 nodes from this test")

    // Verify individual access checks match the query results
    for _, config := range nodeConfigs {
        hasAccess, err := permissionService.CheckNodeAccess(ctx, userId, config.nodeUuid, organizationId)
        assert.NoError(t, err)

        expectedAccess := nodeMap[config.nodeUuid]
        assert.Equal(t, expectedAccess, hasAccess, "Access check for %s should match query result", config.nodeUuid)
    }

    // Clean up all nodes
    for _, config := range nodeConfigs {
        err := nodeAccessStore.RemoveAllNodeAccess(ctx, config.nodeUuid)
        assert.NoError(t, err)
    }
}

func TestPermissionWorkflow_NodeDeletion(t *testing.T) {
    permissionService, nodeAccessStore, testId := setupPermissionIntegrationTest(t)

    ctx := context.Background()
    nodeUuid := "integration-test-node-deletion-" + testId
    ownerId := "owner-delete-test-" + testId
    organizationId := "org-delete-test-" + testId

    // Create node with multiple access entries
    req := models.NodeAccessRequest{
        NodeUuid:        nodeUuid,
        AccessScope:     models.AccessScopeShared,
        SharedWithUsers: []string{"user-1", "user-2", "user-3"},
        SharedWithTeams: []string{"team-1", "team-2"},
    }

    err := permissionService.SetNodePermissions(ctx, nodeUuid, req, ownerId, organizationId, ownerId)
    require.NoError(t, err)

    // Verify multiple access entries exist
    accessList, err := nodeAccessStore.GetNodeAccess(ctx, nodeUuid)
    assert.NoError(t, err)
    assert.Greater(t, len(accessList), 3, "Should have multiple access entries")

    // Simulate node deletion by removing all access
    err = nodeAccessStore.RemoveAllNodeAccess(ctx, nodeUuid)
    assert.NoError(t, err)

    // Verify all access was removed
    accessList, err = nodeAccessStore.GetNodeAccess(ctx, nodeUuid)
    assert.NoError(t, err)
    assert.Len(t, accessList, 0, "All access should be removed")

    // Verify individual access checks return false
    testUsers := []string{ownerId, "user-1", "user-2", "user-3"}
    for _, userId := range testUsers {
        hasAccess, err := permissionService.CheckNodeAccess(ctx, userId, nodeUuid, organizationId)
        assert.NoError(t, err)
        assert.False(t, hasAccess, "User %s should not have access after deletion", userId)
    }
}

func TestPermissionWorkflow_BatchOperations(t *testing.T) {
    permissionService, nodeAccessStore, testId := setupPermissionIntegrationTest(t)

    ctx := context.Background()
    nodeUuid := "integration-test-node-batch-" + testId
    ownerId := "owner-batch-test-" + testId
    organizationId := "org-batch-test-" + testId

    // Create multiple access entries in batch
    accesses := []models.NodeAccess{
        {
            EntityId:       models.FormatEntityId(models.EntityTypeUser, ownerId),
            NodeId:         models.FormatNodeId(nodeUuid),
            EntityType:     models.EntityTypeUser,
            EntityRawId:    ownerId,
            NodeUuid:       nodeUuid,
            AccessType:     models.AccessTypeOwner,
            OrganizationId: organizationId,
            GrantedBy:      ownerId,
        },
        {
            EntityId:       models.FormatEntityId(models.EntityTypeUser, "batch-user-1"),
            NodeId:         models.FormatNodeId(nodeUuid),
            EntityType:     models.EntityTypeUser,
            EntityRawId:    "batch-user-1",
            NodeUuid:       nodeUuid,
            AccessType:     models.AccessTypeShared,
            OrganizationId: organizationId,
            GrantedBy:      ownerId,
        },
        {
            EntityId:       models.FormatEntityId(models.EntityTypeUser, "batch-user-2"),
            NodeId:         models.FormatNodeId(nodeUuid),
            EntityType:     models.EntityTypeUser,
            EntityRawId:    "batch-user-2",
            NodeUuid:       nodeUuid,
            AccessType:     models.AccessTypeShared,
            OrganizationId: organizationId,
            GrantedBy:      ownerId,
        },
        {
            EntityId:       models.FormatEntityId(models.EntityTypeTeam, "batch-team-1"),
            NodeId:         models.FormatNodeId(nodeUuid),
            EntityType:     models.EntityTypeTeam,
            EntityRawId:    "batch-team-1",
            NodeUuid:       nodeUuid,
            AccessType:     models.AccessTypeShared,
            OrganizationId: organizationId,
            GrantedBy:      ownerId,
        },
    }

    // Use batch grant operation
    err := nodeAccessStore.BatchGrantAccess(ctx, accesses)
    assert.NoError(t, err)

    // Verify all access was granted
    accessList, err := nodeAccessStore.GetNodeAccess(ctx, nodeUuid)
    assert.NoError(t, err)
    assert.Len(t, accessList, 4)

    // Verify permissions are correctly reported
    permissions, err := permissionService.GetNodePermissions(ctx, nodeUuid)
    assert.NoError(t, err)
    assert.Equal(t, models.AccessScopeShared, permissions.AccessScope)
    assert.Equal(t, ownerId, permissions.Owner)
    assert.Contains(t, permissions.SharedWithUsers, "batch-user-1")
    assert.Contains(t, permissions.SharedWithUsers, "batch-user-2")
    assert.Contains(t, permissions.SharedWithTeams, "batch-team-1")

    // Test batch access check
    entityIds := []string{
        models.FormatEntityId(models.EntityTypeUser, "batch-user-1"),
        models.FormatEntityId(models.EntityTypeUser, "batch-user-2"),
        models.FormatEntityId(models.EntityTypeTeam, "batch-team-1"),
    }

    hasAccess, err := nodeAccessStore.BatchCheckAccess(ctx, entityIds, models.FormatNodeId(nodeUuid))
    assert.NoError(t, err)
    assert.True(t, hasAccess, "At least one entity should have access")

    // Test batch check with no access
    noAccessEntityIds := []string{
        models.FormatEntityId(models.EntityTypeUser, "no-access-user"),
        models.FormatEntityId(models.EntityTypeTeam, "no-access-team"),
    }

    hasAccess, err = nodeAccessStore.BatchCheckAccess(ctx, noAccessEntityIds, models.FormatNodeId(nodeUuid))
    assert.NoError(t, err)
    assert.False(t, hasAccess, "No entities should have access")

    // Clean up
    err = nodeAccessStore.RemoveAllNodeAccess(ctx, nodeUuid)
    assert.NoError(t, err)
}

func TestPermissionWorkflow_AccessScopeUpdates(t *testing.T) {
    _, nodeAccessStore, testId := setupPermissionIntegrationTest(t)

    ctx := context.Background()
    nodeUuid := "integration-test-scope-updates-" + testId
    organizationId := "org-scope-test-" + testId
    grantedBy := "admin-user-" + testId

    // Test direct access scope updates (bypassing permission service)

    // Set to workspace scope
    err := nodeAccessStore.UpdateNodeAccessScope(ctx, nodeUuid, models.AccessScopeWorkspace, organizationId, grantedBy)
    assert.NoError(t, err)

    // Verify workspace access exists
    workspaceEntityId := models.FormatEntityId(models.EntityTypeWorkspace, organizationId)
    nodeId := models.FormatNodeId(nodeUuid)
    hasAccess, err := nodeAccessStore.HasAccess(ctx, workspaceEntityId, nodeId)
    assert.NoError(t, err)
    assert.True(t, hasAccess)

    // Set to private scope (should remove workspace access)
    err = nodeAccessStore.UpdateNodeAccessScope(ctx, nodeUuid, models.AccessScopePrivate, organizationId, grantedBy)
    assert.NoError(t, err)

    // Verify workspace access was removed
    hasAccess, err = nodeAccessStore.HasAccess(ctx, workspaceEntityId, nodeId)
    assert.NoError(t, err)
    assert.False(t, hasAccess)

    // Set back to workspace scope
    err = nodeAccessStore.UpdateNodeAccessScope(ctx, nodeUuid, models.AccessScopeWorkspace, organizationId, grantedBy)
    assert.NoError(t, err)

    // Verify workspace access was re-granted
    hasAccess, err = nodeAccessStore.HasAccess(ctx, workspaceEntityId, nodeId)
    assert.NoError(t, err)
    assert.True(t, hasAccess)

    // Clean up
    err = nodeAccessStore.RevokeAccess(ctx, workspaceEntityId, nodeId)
    assert.NoError(t, err)
}
