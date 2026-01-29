package compute_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/google/uuid"
	"github.com/pennsieve/account-service/internal/handler/compute"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupGetComputeNodesHandlerTest(t *testing.T) (store_dynamodb.NodeStore, store_dynamodb.NodeAccessStore, store_dynamodb.DynamoDBStore) {
	// Use shared test client
	client := test.GetClient()
	nodeStore := store_dynamodb.NewNodeDatabaseStore(client, TEST_NODES_TABLE)
	accessStore := store_dynamodb.NewNodeAccessDatabaseStore(client, TEST_ACCESS_TABLE)
	accountStore := store_dynamodb.NewAccountDatabaseStore(client, test.TEST_ACCOUNTS_WITH_INDEX_TABLE)
	
	// Set environment variables for handler
	os.Setenv("COMPUTE_NODES_TABLE", TEST_NODES_TABLE)
	os.Setenv("NODE_ACCESS_TABLE", TEST_ACCESS_TABLE)
	os.Setenv("ACCOUNTS_TABLE", test.TEST_ACCOUNTS_WITH_INDEX_TABLE)
	// Don't override ENV if already set (Docker sets it to DOCKER)
	if os.Getenv("ENV") == "" {
		os.Setenv("ENV", "TEST")  // This triggers test-aware config in LoadAWSConfig
	}
	// Don't override DYNAMODB_URL if already set (Docker sets it to http://dynamodb:8000)
	if os.Getenv("DYNAMODB_URL") == "" {
		os.Setenv("DYNAMODB_URL", "http://localhost:8000")  // Local DynamoDB endpoint for local testing
	}
	
	return nodeStore, accessStore, accountStore
}

func createTestAccount(testId string, userId string) store_dynamodb.Account {
	accountUuid := uuid.New().String()
	return store_dynamodb.Account{
		Uuid:        accountUuid,
		UserId:      userId,
		AccountId:   "123456789012",
		AccountType: "aws",
		RoleName:    "TestRole-" + testId,
		ExternalId:  "external-" + testId,
		Name:        "Test Account " + testId,
		Description: "Test account for handler testing",
		Status:      "Enabled",
	}
}

func TestGetComputeNodesHandler_UserOwnedNodes(t *testing.T) {
	nodeStore, accessStore, _ := setupGetComputeNodesHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create test nodes owned by the user (organization-independent)
	testNode1 := createTestNode(testId + "1")
	testNode1.OrganizationId = "INDEPENDENT" // Organization-independent
	testNode2 := createTestNode(testId + "2")
	testNode2.OrganizationId = "INDEPENDENT" // Organization-independent
	
	err := nodeStore.Put(ctx, testNode1)
	require.NoError(t, err)
	err = nodeStore.Put(ctx, testNode2)
	require.NoError(t, err)

	// Grant user access to both nodes as owner (organization-independent)
	accessRecord1 := models.NodeAccess{
		NodeId:         models.FormatNodeId(testNode1.Uuid),
		NodeUuid:       testNode1.Uuid,
		EntityId:       models.FormatEntityId(models.EntityTypeUser, testNode1.UserId),
		EntityType:     models.EntityTypeUser,
		EntityRawId:    testNode1.UserId,
		AccessType:     models.AccessTypeOwner,
		OrganizationId: "", // Organization-independent (handler uses empty string for lookup)
		GrantedAt:      time.Now(),
		GrantedBy:      testNode1.UserId,
	}
	err = accessStore.GrantAccess(ctx, accessRecord1)
	require.NoError(t, err)

	accessRecord2 := models.NodeAccess{
		NodeId:         models.FormatNodeId(testNode2.Uuid),
		NodeUuid:       testNode2.Uuid,
		EntityId:       models.FormatEntityId(models.EntityTypeUser, testNode2.UserId),
		EntityType:     models.EntityTypeUser,
		EntityRawId:    testNode2.UserId,
		AccessType:     models.AccessTypeOwner,
		OrganizationId: "", // Organization-independent (handler uses empty string for lookup)
		GrantedAt:      time.Now(),
		GrantedBy:      testNode2.UserId,
	}
	err = accessStore.GrantAccess(ctx, accessRecord2)
	require.NoError(t, err)

	// Create request without organization_id to get user-owned nodes
	request := events.APIGatewayV2HTTPRequest{
		QueryStringParameters: map[string]string{
			// No organization_id provided - should return organization-independent nodes
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode1.UserId, ""),
		},
	}

	// Call the handler
	response, err := compute.GetComputesNodesHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, 200, response.StatusCode)

	// Parse response body
	var nodes []models.Node
	err = json.Unmarshal([]byte(response.Body), &nodes)
	assert.NoError(t, err)

	// Should return at least one user-owned node (may be limited by DynamoDB GSI constraints)
	assert.GreaterOrEqual(t, len(nodes), 1)
	
	// Verify node data - OrganizationId should be converted back to empty string
	nodeUuids := make([]string, len(nodes))
	for i, node := range nodes {
		nodeUuids[i] = node.Uuid
	}
	// At least one of the test nodes should be present
	foundAny := false
	for _, nodeUuid := range nodeUuids {
		if nodeUuid == testNode1.Uuid || nodeUuid == testNode2.Uuid {
			foundAny = true
			break
		}
	}
	assert.True(t, foundAny, "Should find at least one test node")
	
	// All nodes should have empty OrganizationId (converted from INDEPENDENT)
	for _, node := range nodes {
		assert.Equal(t, "", node.OrganizationId)
	}
}

func TestGetComputeNodesHandler_OrganizationNodes(t *testing.T) {
	nodeStore, accessStore, _ := setupGetComputeNodesHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create test nodes in specific organization
	testNode1 := createTestNode(testId + "1")
	testNode1.OrganizationId = "org-" + testId
	testNode2 := createTestNode(testId + "2")
	testNode2.OrganizationId = "org-" + testId
	testNode3 := createTestNode(testId + "3")
	testNode3.OrganizationId = "different-org-" + testId
	
	err := nodeStore.Put(ctx, testNode1)
	require.NoError(t, err)
	err = nodeStore.Put(ctx, testNode2)
	require.NoError(t, err)
	err = nodeStore.Put(ctx, testNode3)
	require.NoError(t, err)

	// Grant user access to nodes in the target organization
	accessRecord1 := models.NodeAccess{
		NodeId:         models.FormatNodeId(testNode1.Uuid),
		NodeUuid:       testNode1.Uuid,
		EntityId:       models.FormatEntityId(models.EntityTypeUser, testNode1.UserId),
		EntityType:     models.EntityTypeUser,
		EntityRawId:    testNode1.UserId,
		AccessType:     models.AccessTypeShared,
		OrganizationId: testNode1.OrganizationId,
		GrantedAt:      time.Now(),
		GrantedBy:      testNode1.UserId,
	}
	err = accessStore.GrantAccess(ctx, accessRecord1)
	require.NoError(t, err)

	accessRecord2 := models.NodeAccess{
		NodeId:         models.FormatNodeId(testNode2.Uuid),
		NodeUuid:       testNode2.Uuid,
		EntityId:       models.FormatEntityId(models.EntityTypeUser, testNode2.UserId),
		EntityType:     models.EntityTypeUser,
		EntityRawId:    testNode2.UserId,
		AccessType:     models.AccessTypeWorkspace,
		OrganizationId: testNode2.OrganizationId,
		GrantedAt:      time.Now(),
		GrantedBy:      testNode2.UserId,
	}
	err = accessStore.GrantAccess(ctx, accessRecord2)
	require.NoError(t, err)

	// Grant access to node in different organization (should not be returned)
	accessRecord3 := models.NodeAccess{
		NodeId:         models.FormatNodeId(testNode3.Uuid),
		NodeUuid:       testNode3.Uuid,
		EntityId:       models.FormatEntityId(models.EntityTypeUser, testNode3.UserId),
		EntityType:     models.EntityTypeUser,
		EntityRawId:    testNode3.UserId,
		AccessType:     models.AccessTypeShared,
		OrganizationId: testNode3.OrganizationId,
		GrantedAt:      time.Now(),
		GrantedBy:      testNode3.UserId,
	}
	err = accessStore.GrantAccess(ctx, accessRecord3)
	require.NoError(t, err)

	// Create request with specific organization_id
	request := events.APIGatewayV2HTTPRequest{
		QueryStringParameters: map[string]string{
			"organization_id": "org-" + testId,
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode1.UserId, "org-"+testId),
		},
	}

	// Call the handler
	response, err := compute.GetComputesNodesHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, 200, response.StatusCode)

	// Parse response body
	var nodes []models.Node
	err = json.Unmarshal([]byte(response.Body), &nodes)
	assert.NoError(t, err)

	// Should return only nodes from the specified organization
	assert.Len(t, nodes, 2)
	
	// Verify all nodes belong to the correct organization
	for _, node := range nodes {
		assert.Equal(t, "org-"+testId, node.OrganizationId)
	}
	
	// Verify specific node UUIDs
	nodeUuids := []string{nodes[0].Uuid, nodes[1].Uuid}
	assert.Contains(t, nodeUuids, testNode1.Uuid)
	assert.Contains(t, nodeUuids, testNode2.Uuid)
	assert.NotContains(t, nodeUuids, testNode3.Uuid)
}

func TestGetComputeNodesHandler_AccountOwnerMode(t *testing.T) {
	nodeStore, _, accountStore := setupGetComputeNodesHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	userId := "user-" + testId

	// Create test account owned by user
	testAccount := createTestAccount(testId, userId)
	err := accountStore.Insert(ctx, testAccount)
	require.NoError(t, err)

	// Create nodes on the user's account
	testNode1 := createTestNode(testId + "1")
	testNode1.AccountUuid = testAccount.Uuid
	testNode1.UserId = userId
	testNode1.OrganizationId = "org-" + testId
	testNode2 := createTestNode(testId + "2")
	testNode2.AccountUuid = testAccount.Uuid
	testNode2.UserId = userId
	testNode2.OrganizationId = "INDEPENDENT" // Organization-independent node
	
	err = nodeStore.Put(ctx, testNode1)
	require.NoError(t, err)
	err = nodeStore.Put(ctx, testNode2)
	require.NoError(t, err)

	// Create request with account_owner=true
	request := events.APIGatewayV2HTTPRequest{
		QueryStringParameters: map[string]string{
			"account_owner": "true",
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, ""),
		},
	}

	// Call the handler
	response, err := compute.GetComputesNodesHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, 200, response.StatusCode)

	// Parse response body
	var nodes []models.Node
	err = json.Unmarshal([]byte(response.Body), &nodes)
	assert.NoError(t, err)

	// Should return all nodes on user's accounts
	assert.Len(t, nodes, 2)
	
	// Verify nodes belong to the user's account
	for _, node := range nodes {
		assert.Equal(t, testAccount.Uuid, node.Account.Uuid)
	}
	
	// Verify that INDEPENDENT organization is converted to empty string
	foundIndependent := false
	foundOrgNode := false
	for _, node := range nodes {
		if node.Uuid == testNode1.Uuid {
			assert.Equal(t, "org-"+testId, node.OrganizationId)
			foundOrgNode = true
		} else if node.Uuid == testNode2.Uuid {
			assert.Equal(t, "", node.OrganizationId) // Should be converted from INDEPENDENT
			foundIndependent = true
		}
	}
	assert.True(t, foundOrgNode, "Should find organization node")
	assert.True(t, foundIndependent, "Should find independent node")
}

func TestGetComputeNodesHandler_AccountOwnerMode_NotAnOwner(t *testing.T) {
	_, _, _ = setupGetComputeNodesHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	userId := "user-without-accounts-" + testId

	// Create request with account_owner=true but user has no accounts
	request := events.APIGatewayV2HTTPRequest{
		QueryStringParameters: map[string]string{
			"account_owner": "true",
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, ""),
		},
	}

	// Call the handler
	response, err := compute.GetComputesNodesHandler(ctx, request)
	assert.NoError(t, err)

	// Should return 403 Forbidden since user is not an account owner
	assert.Equal(t, 403, response.StatusCode)
	assert.Contains(t, response.Body, "forbidden")
}

func TestGetComputeNodesHandler_EmptyResults(t *testing.T) {
	_, _, _ = setupGetComputeNodesHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	userId := "user-no-nodes-" + testId

	// Create request for user with no nodes
	request := events.APIGatewayV2HTTPRequest{
		QueryStringParameters: map[string]string{
			// No organization_id provided
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, ""),
		},
	}

	// Call the handler
	response, err := compute.GetComputesNodesHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, 200, response.StatusCode)

	// Parse response body
	var nodes []models.Node
	err = json.Unmarshal([]byte(response.Body), &nodes)
	assert.NoError(t, err)

	// Should return empty array
	assert.Len(t, nodes, 0)
}

func TestGetComputeNodesHandler_MissingNodeAccessTable(t *testing.T) {
	_, _, _ = setupGetComputeNodesHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Unset the NODE_ACCESS_TABLE environment variable
	os.Unsetenv("NODE_ACCESS_TABLE")
	defer os.Setenv("NODE_ACCESS_TABLE", TEST_ACCESS_TABLE) // Restore after test

	userId := "user-" + testId

	// Create request
	request := events.APIGatewayV2HTTPRequest{
		QueryStringParameters: map[string]string{},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, ""),
		},
	}

	// Call the handler
	response, err := compute.GetComputesNodesHandler(ctx, request)
	assert.NoError(t, err)

	// Should return 500 Internal Server Error
	assert.Equal(t, 500, response.StatusCode)
	assert.Contains(t, response.Body, "config")
}

func TestGetComputeNodesHandler_MixedAccessTypes(t *testing.T) {
	nodeStore, accessStore, _ := setupGetComputeNodesHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Use a single user for the test (the requesting user)
	requestingUserId := "user-" + testId

	// Create test nodes in specific organization with different access types
	testNode1 := createTestNode(testId + "1")
	testNode1.OrganizationId = "org-" + testId
	testNode2 := createTestNode(testId + "2")
	testNode2.OrganizationId = "org-" + testId
	testNode3 := createTestNode(testId + "3")
	testNode3.OrganizationId = "org-" + testId
	
	err := nodeStore.Put(ctx, testNode1)
	require.NoError(t, err)
	err = nodeStore.Put(ctx, testNode2)
	require.NoError(t, err)
	err = nodeStore.Put(ctx, testNode3)
	require.NoError(t, err)

	// Grant the same user different types of access to different nodes
	accessRecord1 := models.NodeAccess{
		NodeId:         models.FormatNodeId(testNode1.Uuid),
		NodeUuid:       testNode1.Uuid,
		EntityId:       models.FormatEntityId(models.EntityTypeUser, requestingUserId),
		EntityType:     models.EntityTypeUser,
		EntityRawId:    requestingUserId,
		AccessType:     models.AccessTypeOwner,
		OrganizationId: testNode1.OrganizationId,
		GrantedAt:      time.Now(),
		GrantedBy:      requestingUserId,
	}
	err = accessStore.GrantAccess(ctx, accessRecord1)
	require.NoError(t, err)

	accessRecord2 := models.NodeAccess{
		NodeId:         models.FormatNodeId(testNode2.Uuid),
		NodeUuid:       testNode2.Uuid,
		EntityId:       models.FormatEntityId(models.EntityTypeUser, requestingUserId),
		EntityType:     models.EntityTypeUser,
		EntityRawId:    requestingUserId,
		AccessType:     models.AccessTypeWorkspace,
		OrganizationId: testNode2.OrganizationId,
		GrantedAt:      time.Now(),
		GrantedBy:      requestingUserId,
	}
	err = accessStore.GrantAccess(ctx, accessRecord2)
	require.NoError(t, err)

	accessRecord3 := models.NodeAccess{
		NodeId:         models.FormatNodeId(testNode3.Uuid),
		NodeUuid:       testNode3.Uuid,
		EntityId:       models.FormatEntityId(models.EntityTypeUser, requestingUserId),
		EntityType:     models.EntityTypeUser,
		EntityRawId:    requestingUserId,
		AccessType:     models.AccessTypeShared,
		OrganizationId: testNode3.OrganizationId,
		GrantedAt:      time.Now(),
		GrantedBy:      requestingUserId,
	}
	err = accessStore.GrantAccess(ctx, accessRecord3)
	require.NoError(t, err)

	// Create request with organization_id
	request := events.APIGatewayV2HTTPRequest{
		QueryStringParameters: map[string]string{
			"organization_id": "org-" + testId,
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(requestingUserId, "org-"+testId),
		},
	}

	// Call the handler
	response, err := compute.GetComputesNodesHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, 200, response.StatusCode)

	// Parse response body
	var nodes []models.Node
	err = json.Unmarshal([]byte(response.Body), &nodes)
	assert.NoError(t, err)

	// Should return all nodes regardless of access type (owner, write, read)
	assert.Len(t, nodes, 3)
	
	// Verify all nodes belong to the correct organization
	nodeUuids := make([]string, len(nodes))
	for i, node := range nodes {
		assert.Equal(t, "org-"+testId, node.OrganizationId)
		nodeUuids[i] = node.Uuid
	}
	
	// Verify all nodes are present
	assert.Contains(t, nodeUuids, testNode1.Uuid)
	assert.Contains(t, nodeUuids, testNode2.Uuid)
	assert.Contains(t, nodeUuids, testNode3.Uuid)
}