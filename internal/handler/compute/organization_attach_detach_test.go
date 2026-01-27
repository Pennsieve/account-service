package compute

import (
    "context"
    "encoding/json"
    "net/http"
    "os"
    "testing"
    "time"

    "github.com/aws/aws-lambda-go/events"
    "github.com/pennsieve/account-service/internal/models"
    "github.com/pennsieve/account-service/internal/store_dynamodb"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func createAttachDetachTestRequest(method, nodeId, userId, organizationId string) events.APIGatewayV2HTTPRequest {
    path := "/compute-nodes/" + nodeId + "/organization"
    request := events.APIGatewayV2HTTPRequest{
        RouteKey: method + " " + path,
        RawPath:  path,
        PathParameters: map[string]string{
            "id": nodeId,
        },
        RequestContext: events.APIGatewayV2HTTPRequestContext{
            HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
                Method: method,
            },
            Authorizer: &events.APIGatewayV2HTTPRequestContextAuthorizerDescription{
                Lambda: map[string]interface{}{
                    "user_claim": map[string]interface{}{
                        "Id":           float64(123),
                        "NodeId":       userId,
                        "IsSuperAdmin": false,
                    },
                    "org_claim": map[string]interface{}{
                        "NodeId": organizationId,
                        "IntId":  float64(456),
                        "Role":   float64(16), // Admin role as numeric value
                    },
                    "iat": float64(1640995200),
                    "exp": float64(1640998800),
                },
            },
        },
    }

    // Add query parameters for attach operations
    if method == "POST" && organizationId != "" {
        request.QueryStringParameters = map[string]string{
            "organization_id": organizationId,
        }
    }

    return request
}

func TestAttachNodeToOrganizationHandler_MissingNodeId(t *testing.T) {
    request := createAttachDetachTestRequest("POST", "", "user-123", "org-456")
    request.PathParameters = map[string]string{} // Empty path parameters

    response, err := AttachNodeToOrganizationHandler(context.Background(), request)

    assert.NoError(t, err)
    assert.Equal(t, http.StatusBadRequest, response.StatusCode)
    assert.Contains(t, response.Body, "missing node uuid")
}

func TestAttachNodeToOrganizationHandler_MissingOrganizationId(t *testing.T) {
    // Create request without organization ID in authorizer
    request := createAttachDetachTestRequest("POST", "node-123", "user-123", "")
    request.RequestContext.Authorizer.Lambda["organization_node_id"] = ""

    response, err := AttachNodeToOrganizationHandler(context.Background(), request)

    assert.NoError(t, err)
    assert.Equal(t, http.StatusBadRequest, response.StatusCode)
    assert.Contains(t, response.Body, "not found")
}

func TestDetachNodeFromOrganizationHandler_MissingNodeId(t *testing.T) {
    request := createAttachDetachTestRequest("DELETE", "", "user-123", "org-456")
    request.PathParameters = map[string]string{} // Empty path parameters

    response, err := DetachNodeFromOrganizationHandler(context.Background(), request)

    assert.NoError(t, err)
    assert.Equal(t, http.StatusBadRequest, response.StatusCode)
    assert.Contains(t, response.Body, "missing node uuid")
}

func TestAttachNodeToOrganizationHandler_OnlyOwnerCanAttach(t *testing.T) {
    client, err := setupTestClient()
    require.NoError(t, err)

    err = createTestTables(client, "AttachHandler_OnlyOwnerCanAttach")
    require.NoError(t, err)

    // Set environment variables
    os.Setenv("ENV", "TEST")

    defer func() {
        os.Unsetenv("ENV")
        os.Unsetenv("NODE_ACCESS_TABLE")
    }()

    // Create test data - node exists but user doesn't have access
    nodeAccessTable := os.Getenv("NODE_ACCESS_TABLE")
    nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(client, nodeAccessTable)

    nodeUuid := "node-123"
    ownerUserId := "user-owner"
    nonOwnerUserId := "user-non-owner"
    organizationId := "org-456"

    // Create node access for actual owner
    nodeAccess := models.NodeAccess{
        NodeId:         models.FormatNodeId(nodeUuid),
        EntityId:       models.FormatEntityId(models.EntityTypeUser, ownerUserId),
        AccessType:     models.AccessTypeOwner,
        OrganizationId: "", // Organization-independent node
    }
    err = nodeAccessStore.GrantAccess(context.Background(), nodeAccess)
    require.NoError(t, err)

    // Create request from non-owner user
    request := createAttachDetachTestRequest("POST", nodeUuid, nonOwnerUserId, organizationId)
    request.QueryStringParameters = map[string]string{
        "organization_id": organizationId,
    }

    response, err := AttachNodeToOrganizationHandler(context.Background(), request)

    assert.NoError(t, err)
    assert.Equal(t, http.StatusForbidden, response.StatusCode)
    assert.Contains(t, response.Body, "forbidden")
}

func TestAttachNodeToOrganizationHandler_NodeAlreadyHasOrganization(t *testing.T) {
    client, err := setupTestClient()
    require.NoError(t, err)

    err = createTestTables(client, "AttachHandler_NodeAlreadyHasOrganization")
    require.NoError(t, err)

    // Set environment variables
    os.Setenv("ENV", "TEST")

    defer func() {
        os.Unsetenv("ENV")
        os.Unsetenv("NODE_ACCESS_TABLE")
    }()

    // Create test data - node exists and already belongs to an organization
    nodeAccessTable := os.Getenv("NODE_ACCESS_TABLE")
    nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(client, nodeAccessTable)

    nodeUuid := "node-123"
    userId := "user-123"
    currentOrgId := "org-current"
    newOrgId := "org-new"

    // Create node access for owner, but node already belongs to an organization
    nodeAccess := models.NodeAccess{
        NodeId:         models.FormatNodeId(nodeUuid),
        EntityId:       models.FormatEntityId(models.EntityTypeUser, userId),
        EntityType:     models.EntityTypeUser,
        EntityRawId:    userId,
        NodeUuid:       nodeUuid,
        AccessType:     models.AccessTypeOwner,
        OrganizationId: currentOrgId, // Node already belongs to an organization
        GrantedAt:      time.Now(),
        GrantedBy:      userId,
    }
    err = nodeAccessStore.GrantAccess(context.Background(), nodeAccess)
    require.NoError(t, err)

    // Try to attach node to a different organization (should fail)
    request := createAttachDetachTestRequest("POST", nodeUuid, userId, newOrgId)
    request.QueryStringParameters = map[string]string{
        "organization_id": newOrgId,
    }

    response, err := AttachNodeToOrganizationHandler(context.Background(), request)

    assert.NoError(t, err)
    assert.Equal(t, http.StatusBadRequest, response.StatusCode)
    assert.Contains(t, response.Body, "cannot attach node that already belongs to an organization")
}

func TestAttachNodeToOrganizationHandler_Success(t *testing.T) {
    client, err := setupTestClient()
    require.NoError(t, err)

    err = createTestTables(client, "AttachHandler_Success")
    require.NoError(t, err)

    // Set environment variables
    os.Setenv("ENV", "TEST")

    defer func() {
        os.Unsetenv("ENV")
        os.Unsetenv("NODE_ACCESS_TABLE")
    }()

    // Create test data - node exists and user is the owner, node is organization-independent
    nodeAccessTable := os.Getenv("NODE_ACCESS_TABLE")
    nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(client, nodeAccessTable)

    nodeUuid := "node-123"
    userId := "user-123"
    organizationId := "org-456"

    // Create node access for owner - organization-independent node
    nodeAccess := models.NodeAccess{
        NodeId:         models.FormatNodeId(nodeUuid),
        EntityId:       models.FormatEntityId(models.EntityTypeUser, userId),
        EntityType:     models.EntityTypeUser,
        EntityRawId:    userId,
        NodeUuid:       nodeUuid,
        AccessType:     models.AccessTypeOwner,
        OrganizationId: "", // Organization-independent node
        GrantedAt:      time.Now(),
        GrantedBy:      userId,
    }
    err = nodeAccessStore.GrantAccess(context.Background(), nodeAccess)
    require.NoError(t, err)

    // Try to attach node to organization (should succeed)
    request := createAttachDetachTestRequest("POST", nodeUuid, userId, organizationId)
    request.QueryStringParameters = map[string]string{
        "organization_id": organizationId,
    }

    response, err := AttachNodeToOrganizationHandler(context.Background(), request)

    assert.NoError(t, err)
    assert.Equal(t, http.StatusOK, response.StatusCode)
    assert.Contains(t, response.Body, "Successfully attached node to organization")

    // Verify response format
    var responseObj struct {
        Message    string `json:"message"`
        NodeUuid   string `json:"nodeUuid"`
        Action     string `json:"action"`
        EntityType string `json:"entityType"`
        EntityId   string `json:"entityId"`
    }
    err = json.Unmarshal([]byte(response.Body), &responseObj)
    require.NoError(t, err)
    assert.Equal(t, "Successfully attached node to organization", responseObj.Message)
    assert.Equal(t, nodeUuid, responseObj.NodeUuid)
    assert.Equal(t, "attached_to_organization", responseObj.Action)
    assert.Equal(t, "organization", responseObj.EntityType)
    assert.Equal(t, organizationId, responseObj.EntityId)
}

func TestAttachNodeToOrganizationHandler_MissingNodeAccessTable(t *testing.T) {
    // Test case where NODE_ACCESS_TABLE environment variable is not set
    os.Unsetenv("NODE_ACCESS_TABLE")

    defer func() {
        // Restore any previous value
        if val := os.Getenv("NODE_ACCESS_TABLE"); val == "" {
            os.Unsetenv("NODE_ACCESS_TABLE")
        }
    }()

    request := createAttachDetachTestRequest("POST", "node-123", "user-123", "org-456")
    request.QueryStringParameters = map[string]string{
        "organization_id": "org-456",
    }

    response, err := AttachNodeToOrganizationHandler(context.Background(), request)

    assert.NoError(t, err)
    assert.Equal(t, http.StatusInternalServerError, response.StatusCode)
    assert.Contains(t, response.Body, "error loading AWS config")
}

func TestDetachNodeFromOrganizationHandler_OnlyAccountOwnerCanDetach(t *testing.T) {
    client, err := setupTestClient()
    require.NoError(t, err)

    err = createTestTables(client, "DetachHandler_OnlyAccountOwnerCanDetach")
    require.NoError(t, err)

    // Set environment variables
    os.Setenv("ENV", "TEST")

    defer func() {
        os.Unsetenv("ENV")
        os.Unsetenv("NODE_ACCESS_TABLE")
        os.Unsetenv("COMPUTE_NODES_TABLE")
        os.Unsetenv("ACCOUNTS_TABLE")
    }()

    nodeUuid := "node-123"
    nodeOwnerUserId := "node-owner-123"
    accountOwnerUserId := "account-owner-456"
    accountUuid := "account-789"
    organizationId := "org-current"

    // Create account owned by different user than the node owner
    accountStore := store_dynamodb.NewAccountDatabaseStore(client, os.Getenv("ACCOUNTS_TABLE"))
    testAccount := store_dynamodb.Account{
        Uuid:        accountUuid,
        AccountId:   "123456789",
        AccountType: "aws",
        UserId:      accountOwnerUserId, // Different from node owner
        RoleName:    "test-role",
        ExternalId:  "ext-123",
        Name:        "Test Account",
        Status:      "Enabled",
    }
    err = accountStore.Insert(context.Background(), testAccount)
    require.NoError(t, err)

    // Create a compute node that belongs to the account
    nodeStore := store_dynamodb.NewNodeDatabaseStore(client, os.Getenv("COMPUTE_NODES_TABLE"))
    testNode := models.DynamoDBNode{
        Uuid:           nodeUuid,
        Name:           "Test Node",
        Description:    "Test Description",
        AccountUuid:    accountUuid,
        OrganizationId: organizationId, // Node belongs to an organization
        UserId:         nodeOwnerUserId,
        Status:         "Enabled",
    }
    err = nodeStore.Put(context.Background(), testNode)
    require.NoError(t, err)

    // Create node access for the node owner (who is NOT the account owner)
    nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(client, os.Getenv("NODE_ACCESS_TABLE"))
    nodeAccess := models.NodeAccess{
        NodeId:         models.FormatNodeId(nodeUuid),
        EntityId:       models.FormatEntityId(models.EntityTypeUser, nodeOwnerUserId),
        EntityType:     models.EntityTypeUser,
        EntityRawId:    nodeOwnerUserId,
        NodeUuid:       nodeUuid,
        AccessType:     models.AccessTypeOwner,
        OrganizationId: organizationId,
        GrantedAt:      time.Now(),
        GrantedBy:      nodeOwnerUserId,
    }
    err = nodeAccessStore.GrantAccess(context.Background(), nodeAccess)
    require.NoError(t, err)

    // Try to detach node as node owner (who is NOT the account owner) - should fail
    request := createAttachDetachTestRequest("DELETE", nodeUuid, nodeOwnerUserId, organizationId)

    response, err := DetachNodeFromOrganizationHandler(context.Background(), request)

    assert.NoError(t, err)
    assert.Equal(t, http.StatusForbidden, response.StatusCode)
    assert.Contains(t, response.Body, "only the account owner can detach nodes")
}

func TestDetachNodeFromOrganizationHandler_NodeAlreadyIndependent(t *testing.T) {
    client, err := setupTestClient()
    require.NoError(t, err)

    err = createTestTables(client, "DetachHandler_NodeAlreadyIndependent")
    require.NoError(t, err)

    // Set environment variables
    os.Setenv("ENV", "TEST")

    defer func() {
        os.Unsetenv("ENV")
        os.Unsetenv("NODE_ACCESS_TABLE")
        os.Unsetenv("COMPUTE_NODES_TABLE")
        os.Unsetenv("ACCOUNTS_TABLE")
    }()

    nodeUuid := "node-123"
    userId := "user-123"
    accountUuid := "account-789"

    // Create account
    accountStore := store_dynamodb.NewAccountDatabaseStore(client, os.Getenv("ACCOUNTS_TABLE"))
    testAccount := store_dynamodb.Account{
        Uuid:        accountUuid,
        AccountId:   "123456789",
        AccountType: "aws",
        UserId:      userId,
        RoleName:    "test-role",
        ExternalId:  "ext-123",
        Name:        "Test Account",
        Status:      "Enabled",
    }
    err = accountStore.Insert(context.Background(), testAccount)
    require.NoError(t, err)

    // Create a compute node that is organization-independent (no organizationId)
    nodeStore := store_dynamodb.NewNodeDatabaseStore(client, os.Getenv("COMPUTE_NODES_TABLE"))
    testNode := models.DynamoDBNode{
        Uuid:           nodeUuid,
        Name:           "Test Node",
        Description:    "Test Description",
        AccountUuid:    accountUuid,
        OrganizationId: "", // Organization-independent node
        UserId:         userId,
        Status:         "Enabled",
    }
    err = nodeStore.Put(context.Background(), testNode)
    require.NoError(t, err)

    // Create node access for an organization-independent node
    nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(client, os.Getenv("NODE_ACCESS_TABLE"))
    nodeAccess := models.NodeAccess{
        NodeId:         models.FormatNodeId(nodeUuid),
        EntityId:       models.FormatEntityId(models.EntityTypeUser, userId),
        EntityType:     models.EntityTypeUser,
        EntityRawId:    userId,
        NodeUuid:       nodeUuid,
        AccessType:     models.AccessTypeOwner,
        OrganizationId: "", // Organization-independent
        GrantedAt:      time.Now(),
        GrantedBy:      userId,
    }
    err = nodeAccessStore.GrantAccess(context.Background(), nodeAccess)
    require.NoError(t, err)

    // Try to detach an already organization-independent node (should fail)
    request := createAttachDetachTestRequest("DELETE", nodeUuid, userId, "")

    response, err := DetachNodeFromOrganizationHandler(context.Background(), request)

    assert.NoError(t, err)
    assert.Equal(t, http.StatusBadRequest, response.StatusCode)
    assert.Contains(t, response.Body, "organization-independent nodes cannot be shared")
}

func TestDetachNodeFromOrganizationHandler_Success(t *testing.T) {
    client, err := setupTestClient()
    require.NoError(t, err)

    err = createTestTables(client, "DetachHandler_Success")
    require.NoError(t, err)

    // Set environment variables
    os.Setenv("ENV", "TEST")

    defer func() {
        os.Unsetenv("ENV")
        os.Unsetenv("NODE_ACCESS_TABLE")
        os.Unsetenv("COMPUTE_NODES_TABLE")
        os.Unsetenv("ACCOUNTS_TABLE")
    }()

    nodeUuid := "node-123"
    userId := "user-123" // Same user is both account owner and node owner
    accountUuid := "account-789"
    organizationId := "org-current"

    // Create account owned by the user
    accountStore := store_dynamodb.NewAccountDatabaseStore(client, os.Getenv("ACCOUNTS_TABLE"))
    testAccount := store_dynamodb.Account{
        Uuid:        accountUuid,
        AccountId:   "123456789",
        AccountType: "aws",
        UserId:      userId, // Same as node owner - user is account owner
        RoleName:    "test-role",
        ExternalId:  "ext-123",
        Name:        "Test Account",
        Status:      "Enabled",
    }
    err = accountStore.Insert(context.Background(), testAccount)
    require.NoError(t, err)

    // Create a compute node that belongs to an organization
    nodeStore := store_dynamodb.NewNodeDatabaseStore(client, os.Getenv("COMPUTE_NODES_TABLE"))
    testNode := models.DynamoDBNode{
        Uuid:           nodeUuid,
        Name:           "Test Node",
        Description:    "Test Description",
        AccountUuid:    accountUuid,
        OrganizationId: organizationId, // Node belongs to an organization
        UserId:         userId,
        Status:         "Enabled",
    }
    err = nodeStore.Put(context.Background(), testNode)
    require.NoError(t, err)

    // Create node access for the user (who is both node owner and account owner)
    nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(client, os.Getenv("NODE_ACCESS_TABLE"))
    nodeAccess := models.NodeAccess{
        NodeId:         models.FormatNodeId(nodeUuid),
        EntityId:       models.FormatEntityId(models.EntityTypeUser, userId),
        EntityType:     models.EntityTypeUser,
        EntityRawId:    userId,
        NodeUuid:       nodeUuid,
        AccessType:     models.AccessTypeOwner,
        OrganizationId: organizationId,
        GrantedAt:      time.Now(),
        GrantedBy:      userId,
    }
    err = nodeAccessStore.GrantAccess(context.Background(), nodeAccess)
    require.NoError(t, err)

    // Try to detach node as account owner - should succeed
    request := createAttachDetachTestRequest("DELETE", nodeUuid, userId, organizationId)

    response, err := DetachNodeFromOrganizationHandler(context.Background(), request)

    assert.NoError(t, err)
    assert.Equal(t, http.StatusOK, response.StatusCode)
    assert.Contains(t, response.Body, "Successfully detached node from organization")
}

// Integration tests
func TestOrganizationAttachDetach_Integration_FullWorkflow(t *testing.T) {
    t.Skip("Integration test - requires full test environment setup")

    // Full integration test:
    // 1. Create organization-independent node
    // 2. Attach it to organization
    // 3. Verify it can be shared within organization
    // 4. Detach it from organization
    // 5. Verify it becomes private again
}

func TestOrganizationAttachDetach_Integration_PermissionValidation(t *testing.T) {
    t.Skip("Integration test - requires full test environment setup")

    // Full integration test for permission validation:
    // 1. Test attach with non-owner user (should fail)
    // 2. Test detach with non-account-owner user (should fail)
    // 3. Test attach/detach with proper permissions (should succeed)
}

// Test helper functions for setting up test environment
func setupTestEnvironment() {
    os.Setenv("NODE_ACCESS_TABLE", "test-node-access")
    os.Setenv("COMPUTE_NODES_TABLE", "test-nodes")
    os.Setenv("ACCOUNTS_TABLE", "test-accounts")
}

func cleanupTestEnvironment() {
    os.Unsetenv("NODE_ACCESS_TABLE")
    os.Unsetenv("COMPUTE_NODES_TABLE")
    os.Unsetenv("ACCOUNTS_TABLE")
}
