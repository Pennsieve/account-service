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

// Define test table constants
const (
	TEST_NODES_TABLE  = "test-nodes-table"
	TEST_ACCESS_TABLE = "test-access-table"
)

// TestMain for compute handler tests
func TestMain(m *testing.M) {
	// Setup shared test resources before running tests
	if err := test.SetupPackageTables(); err != nil {
		panic(err)
	}
	
	// Run tests
	code := m.Run()
	
	// Exit with test result code
	os.Exit(code)
}

func setupGetComputeNodeHandlerTest(t *testing.T) (store_dynamodb.NodeStore, store_dynamodb.NodeAccessStore) {
	// Use shared test client
	client := test.GetClient()
	nodeStore := store_dynamodb.NewNodeDatabaseStore(client, TEST_NODES_TABLE)
	accessStore := store_dynamodb.NewNodeAccessDatabaseStore(client, TEST_ACCESS_TABLE)
	
	// Set environment variables for handler
	os.Setenv("COMPUTE_NODES_TABLE", TEST_NODES_TABLE)
	os.Setenv("NODE_ACCESS_TABLE", TEST_ACCESS_TABLE)
	// Don't override ENV if already set (Docker sets it to DOCKER)
	if os.Getenv("ENV") == "" {
		os.Setenv("ENV", "TEST")  // This triggers test-aware config in LoadAWSConfig
	}
	// Don't override DYNAMODB_URL if already set (Docker sets it to http://dynamodb:8000)
	if os.Getenv("DYNAMODB_URL") == "" {
		os.Setenv("DYNAMODB_URL", "http://localhost:8000")  // Local DynamoDB endpoint for local testing
	}
	
	// No specific cleanup needed as we use unique test IDs
	
	return nodeStore, accessStore
}

func createTestNode(testId string) models.DynamoDBNode {
	nodeUuid := uuid.New().String()
	return models.DynamoDBNode{
		Uuid:                  nodeUuid,
		Name:                  "Test Compute Node " + testId,
		Description:           "Test compute node for handler testing",
		ComputeNodeGatewayUrl: "https://test-gateway-" + testId + ".example.com",
		EfsId:                 "fs-test-" + testId,
		QueueUrl:              "https://sqs.region.amazonaws.com/123/test-queue-" + testId,
		Env:                   "test",
		AccountUuid:           "account-uuid-" + testId,
		AccountId:             "123456789012",
		AccountType:           "aws",
		CreatedAt:             "2024-01-25T10:00:00Z",
		OrganizationId:        "org-" + testId,
		UserId:                "user-" + testId,
		Identifier:            "node-identifier-" + testId,
		WorkflowManagerTag:    "v1.0.0",
		Status:                "Enabled",
	}
}

func TestGetComputeNodeHandler_Success(t *testing.T) {
	nodeStore, accessStore := setupGetComputeNodeHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create and insert test node
	testNode := createTestNode(testId)
	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	// Grant user access to the node
	accessRecord := models.NodeAccess{
		NodeId:         models.FormatNodeId(testNode.Uuid),
		NodeUuid:       testNode.Uuid,
		EntityId:       models.FormatEntityId(models.EntityTypeUser, testNode.UserId),
		EntityType:     models.EntityTypeUser,
		EntityRawId:    testNode.UserId,
		AccessType:     models.AccessTypeOwner,
		OrganizationId: testNode.OrganizationId,
		GrantedAt:      time.Now(),
		GrantedBy:      testNode.UserId,
	}
	err = accessStore.GrantAccess(ctx, accessRecord)
	require.NoError(t, err)

	// Create request with test authorizer
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testNode.Uuid,
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
		},
	}

	// Call the handler
	response, err := compute.GetComputeNodeHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, 200, response.StatusCode)

	// Parse response body
	var node models.Node
	err = json.Unmarshal([]byte(response.Body), &node)
	assert.NoError(t, err)

	// Verify node data
	assert.Equal(t, testNode.Uuid, node.Uuid)
	assert.Equal(t, testNode.Name, node.Name)
	assert.Equal(t, testNode.Description, node.Description)
	assert.Equal(t, testNode.ComputeNodeGatewayUrl, node.ComputeNodeGatewayUrl)
	assert.Equal(t, testNode.EfsId, node.EfsId)
	assert.Equal(t, testNode.QueueUrl, node.QueueUrl)
	assert.Equal(t, testNode.AccountUuid, node.Account.Uuid)
	assert.Equal(t, testNode.AccountId, node.Account.AccountId)
	assert.Equal(t, testNode.AccountType, node.Account.AccountType)
	assert.Equal(t, testNode.OrganizationId, node.OrganizationId)
	assert.Equal(t, testNode.UserId, node.UserId)
	assert.Equal(t, testNode.WorkflowManagerTag, node.WorkflowManagerTag)
	assert.Equal(t, testNode.Status, node.Status)
}

func TestGetComputeNodeHandler_NotFound(t *testing.T) {
	_, _ = setupGetComputeNodeHandlerTest(t)
	ctx := context.Background()

	nonExistentId := uuid.New().String()

	// Create request for non-existent node with test authorizer
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": nonExistentId,
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("test-user", "test-org"),
		},
	}

	// Call the handler
	response, err := compute.GetComputeNodeHandler(ctx, request)
	assert.NoError(t, err)

	// Verify 404 response
	assert.Equal(t, 404, response.StatusCode)
	assert.Contains(t, response.Body, "no records found")
}

func TestGetComputeNodeHandler_MissingId(t *testing.T) {
	_, _ = setupGetComputeNodeHandlerTest(t)
	ctx := context.Background()

	// Create request without ID parameter but with test authorizer
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("test-user", "test-org"),
		},
	}

	// Call the handler
	response, err := compute.GetComputeNodeHandler(ctx, request)
	assert.NoError(t, err)

	// Should return 400 Bad Request for missing ID
	assert.Equal(t, 400, response.StatusCode)
	assert.Contains(t, response.Body, "bad request")
}

func TestGetComputeNodeHandler_MultipleNodes(t *testing.T) {
	nodeStore, accessStore := setupGetComputeNodeHandlerTest(t)
	ctx := context.Background()
	testId1 := test.GenerateTestId()
	testId2 := test.GenerateTestId()

	// Create and insert multiple test nodes
	testNode1 := createTestNode(testId1)
	testNode2 := createTestNode(testId2)
	
	err := nodeStore.Put(ctx, testNode1)
	require.NoError(t, err)
	err = nodeStore.Put(ctx, testNode2)
	require.NoError(t, err)

	// Grant user access to both nodes
	accessRecord1 := models.NodeAccess{
		NodeId:         models.FormatNodeId(testNode1.Uuid),
		NodeUuid:       testNode1.Uuid,
		EntityId:       models.FormatEntityId(models.EntityTypeUser, testNode1.UserId),
		EntityType:     models.EntityTypeUser,
		EntityRawId:    testNode1.UserId,
		AccessType:     models.AccessTypeOwner,
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
		AccessType:     models.AccessTypeOwner,
		OrganizationId: testNode2.OrganizationId,
		GrantedAt:      time.Now(),
		GrantedBy:      testNode2.UserId,
	}
	err = accessStore.GrantAccess(ctx, accessRecord2)
	require.NoError(t, err)

	// Request first node with test authorizer
	request1 := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testNode1.Uuid,
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode1.UserId, testNode1.OrganizationId),
		},
	}

	response1, err := compute.GetComputeNodeHandler(ctx, request1)
	assert.NoError(t, err)
	assert.Equal(t, 200, response1.StatusCode)

	var node1 models.Node
	err = json.Unmarshal([]byte(response1.Body), &node1)
	assert.NoError(t, err)
	assert.Equal(t, testNode1.Uuid, node1.Uuid)
	assert.Equal(t, testNode1.Name, node1.Name)

	// Request second node with test authorizer
	request2 := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testNode2.Uuid,
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode2.UserId, testNode2.OrganizationId),
		},
	}

	response2, err := compute.GetComputeNodeHandler(ctx, request2)
	assert.NoError(t, err)
	assert.Equal(t, 200, response2.StatusCode)

	var node2 models.Node
	err = json.Unmarshal([]byte(response2.Body), &node2)
	assert.NoError(t, err)
	assert.Equal(t, testNode2.Uuid, node2.Uuid)
	assert.Equal(t, testNode2.Name, node2.Name)
}

func TestGetComputeNodeHandler_NoAccess(t *testing.T) {
	nodeStore, _ := setupGetComputeNodeHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create and insert test node
	testNode := createTestNode(testId)
	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	// NOTE: We deliberately don't grant access to the user

	// Create request with a different user who doesn't have access
	unauthorizedUserId := "unauthorized-user-" + testId
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testNode.Uuid,
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(unauthorizedUserId, testNode.OrganizationId),
		},
	}

	// Call the handler
	response, err := compute.GetComputeNodeHandler(ctx, request)
	assert.NoError(t, err)

	// Should return 403 Forbidden
	assert.Equal(t, 403, response.StatusCode)
	assert.Contains(t, response.Body, "forbidden")
}

func TestGetComputeNodeHandler_DifferentStatuses(t *testing.T) {
	nodeStore, accessStore := setupGetComputeNodeHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Test nodes with different statuses
	testCases := []struct {
		name   string
		status string
	}{
		{"Enabled Node", "Enabled"},
		{"Paused Node", "Paused"},
		{"Provisioning Node", "Provisioning"},
		{"Failed Node", "Failed"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create node with specific status
			testNode := createTestNode(testId)
			testNode.Uuid = uuid.New().String() // Unique UUID for each test case
			testNode.Status = tc.status
			testNode.Name = tc.name

			err := nodeStore.Put(ctx, testNode)
			require.NoError(t, err)

			// Grant user access to the node
			accessRecord := models.NodeAccess{
				NodeId:         models.FormatNodeId(testNode.Uuid),
				NodeUuid:       testNode.Uuid,
				EntityId:       models.FormatEntityId(models.EntityTypeUser, testNode.UserId),
				EntityType:     models.EntityTypeUser,
				EntityRawId:    testNode.UserId,
				AccessType:     models.AccessTypeOwner,
				OrganizationId: testNode.OrganizationId,
				GrantedAt:      time.Now(),
				GrantedBy:      testNode.UserId,
			}
			err = accessStore.GrantAccess(ctx, accessRecord)
			require.NoError(t, err)

			// Request the node with test authorizer
			request := events.APIGatewayV2HTTPRequest{
				PathParameters: map[string]string{
					"id": testNode.Uuid,
				},
				RequestContext: events.APIGatewayV2HTTPRequestContext{
					Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
				},
			}

			response, err := compute.GetComputeNodeHandler(ctx, request)
			assert.NoError(t, err)
			assert.Equal(t, 200, response.StatusCode)

			var node models.Node
			err = json.Unmarshal([]byte(response.Body), &node)
			assert.NoError(t, err)
			assert.Equal(t, tc.status, node.Status, "Status should be %s", tc.status)
		})
	}
}

func TestGetComputeNodeHandler_OrganizationMismatch(t *testing.T) {
	nodeStore, accessStore := setupGetComputeNodeHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create test node belonging to one organization
	testNode := createTestNode(testId)
	testNode.OrganizationId = "org-" + testId
	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	// Grant user access to the node
	accessRecord := models.NodeAccess{
		NodeId:         models.FormatNodeId(testNode.Uuid),
		NodeUuid:       testNode.Uuid,
		EntityId:       models.FormatEntityId(models.EntityTypeUser, testNode.UserId),
		EntityType:     models.EntityTypeUser,
		EntityRawId:    testNode.UserId,
		AccessType:     models.AccessTypeOwner,
		OrganizationId: testNode.OrganizationId,
		GrantedAt:      time.Now(),
		GrantedBy:      testNode.UserId,
	}
	err = accessStore.GrantAccess(ctx, accessRecord)
	require.NoError(t, err)

	// Create request with different organization in query parameters
	differentOrgId := "different-org-" + testId
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testNode.Uuid,
		},
		QueryStringParameters: map[string]string{
			"organization_id": differentOrgId,
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, ""),
		},
	}

	// Call the handler
	response, err := compute.GetComputeNodeHandler(ctx, request)
	assert.NoError(t, err)

	// Should return 403 Forbidden due to organization mismatch
	assert.Equal(t, 403, response.StatusCode)
	assert.Contains(t, response.Body, "forbidden")
}

func TestGetComputeNodeHandler_UserOwnedNode_NoOrgClaim(t *testing.T) {
	nodeStore, accessStore := setupGetComputeNodeHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create test node with no organization (user-owned)
	testNode := createTestNode(testId)
	testNode.OrganizationId = "INDEPENDENT" // User-owned node (DynamoDB GSI doesn't allow empty strings)
	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	// Grant user access to the node (organization-independent)
	accessRecord := models.NodeAccess{
		NodeId:         models.FormatNodeId(testNode.Uuid),
		NodeUuid:       testNode.Uuid,
		EntityId:       models.FormatEntityId(models.EntityTypeUser, testNode.UserId),
		EntityType:     models.EntityTypeUser,
		EntityRawId:    testNode.UserId,
		AccessType:     models.AccessTypeOwner,
		OrganizationId: "INDEPENDENT", // Organization-independent (DynamoDB GSI doesn't allow empty strings)
		GrantedAt:      time.Now(),
		GrantedBy:      testNode.UserId,
	}
	err = accessStore.GrantAccess(ctx, accessRecord)
	require.NoError(t, err)

	// Create request with no organization in claims (empty string)
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testNode.Uuid,
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, ""), // No org claim
		},
	}

	// Call the handler
	response, err := compute.GetComputeNodeHandler(ctx, request)
	assert.NoError(t, err)

	// Should return 200 OK for user-owned node
	assert.Equal(t, 200, response.StatusCode)

	// Parse response body
	var node models.Node
	err = json.Unmarshal([]byte(response.Body), &node)
	assert.NoError(t, err)
	assert.Equal(t, testNode.Uuid, node.Uuid)
	assert.Equal(t, "", node.OrganizationId) // Should be empty
}

func TestGetComputeNodeHandler_AccountPausedOverridesNodeStatus(t *testing.T) {
	nodeStore, accessStore := setupGetComputeNodeHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Set up ACCOUNTS_TABLE environment variable for account status check
	os.Setenv("ACCOUNTS_TABLE", test.TEST_ACCOUNTS_WITH_INDEX_TABLE)

	// Create and insert test node with Enabled status
	testNode := createTestNode(testId)
	testNode.Status = "Enabled"
	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	// Create and insert a paused account
	client := test.GetClient()
	accountStore := store_dynamodb.NewAccountDatabaseStore(client, test.TEST_ACCOUNTS_WITH_INDEX_TABLE)
	testAccount := store_dynamodb.Account{
		Uuid:        testNode.AccountUuid,
		AccountId:   testNode.AccountId,
		AccountType: testNode.AccountType,
		UserId:      testNode.UserId,
		Name:        "Test Account",
		Description: "Test account description",
		Status:      "Paused",
	}
	accountStoreImpl, ok := accountStore.(*store_dynamodb.AccountDatabaseStore)
	require.True(t, ok)
	err = accountStoreImpl.Insert(ctx, testAccount)
	require.NoError(t, err)

	// Grant user access to the node
	accessRecord := models.NodeAccess{
		NodeId:         models.FormatNodeId(testNode.Uuid),
		NodeUuid:       testNode.Uuid,
		EntityId:       models.FormatEntityId(models.EntityTypeUser, testNode.UserId),
		EntityType:     models.EntityTypeUser,
		EntityRawId:    testNode.UserId,
		AccessType:     models.AccessTypeOwner,
		OrganizationId: testNode.OrganizationId,
		GrantedAt:      time.Now(),
		GrantedBy:      testNode.UserId,
	}
	err = accessStore.GrantAccess(ctx, accessRecord)
	require.NoError(t, err)

	// Create request with test authorizer
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testNode.Uuid,
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
		},
	}

	// Call the handler
	response, err := compute.GetComputeNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 200, response.StatusCode)

	// Parse response and verify node status is overridden to Paused
	var nodeResponse models.Node
	err = json.Unmarshal([]byte(response.Body), &nodeResponse)
	assert.NoError(t, err)
	assert.Equal(t, "Paused", nodeResponse.Status) // Should be Paused due to account status
}

func TestGetComputeNodeHandler_PendingNodeIgnoresAccountStatus(t *testing.T) {
	nodeStore, accessStore := setupGetComputeNodeHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Set up ACCOUNTS_TABLE environment variable for account status check
	os.Setenv("ACCOUNTS_TABLE", test.TEST_ACCOUNTS_WITH_INDEX_TABLE)

	// Create and insert test node with Pending status
	testNode := createTestNode(testId)
	testNode.Status = "Pending"
	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	// Create and insert a paused account
	client := test.GetClient()
	accountStore := store_dynamodb.NewAccountDatabaseStore(client, test.TEST_ACCOUNTS_WITH_INDEX_TABLE)
	testAccount := store_dynamodb.Account{
		Uuid:        testNode.AccountUuid,
		AccountId:   testNode.AccountId,
		AccountType: testNode.AccountType,
		UserId:      testNode.UserId,
		Name:        "Test Account",
		Description: "Test account description",
		Status:      "Paused",
	}
	accountStoreImpl, ok := accountStore.(*store_dynamodb.AccountDatabaseStore)
	require.True(t, ok)
	err = accountStoreImpl.Insert(ctx, testAccount)
	require.NoError(t, err)

	// Grant user access to the node
	accessRecord := models.NodeAccess{
		NodeId:         models.FormatNodeId(testNode.Uuid),
		NodeUuid:       testNode.Uuid,
		EntityId:       models.FormatEntityId(models.EntityTypeUser, testNode.UserId),
		EntityType:     models.EntityTypeUser,
		EntityRawId:    testNode.UserId,
		AccessType:     models.AccessTypeOwner,
		OrganizationId: testNode.OrganizationId,
		GrantedAt:      time.Now(),
		GrantedBy:      testNode.UserId,
	}
	err = accessStore.GrantAccess(ctx, accessRecord)
	require.NoError(t, err)

	// Create request with test authorizer
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testNode.Uuid,
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
		},
	}

	// Call the handler
	response, err := compute.GetComputeNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 200, response.StatusCode)

	// Parse response and verify node status remains Pending
	var nodeResponse models.Node
	err = json.Unmarshal([]byte(response.Body), &nodeResponse)
	assert.NoError(t, err)
	assert.Equal(t, "Pending", nodeResponse.Status) // Should remain Pending despite account being Paused
}