package compute_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/google/uuid"
	"github.com/pennsieve/account-service/internal/handler/compute"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupPatchComputeNodeHandlerTest(t *testing.T) store_dynamodb.NodeStore {
	// Use shared test client
	client := test.GetClient()
	nodeStore := store_dynamodb.NewNodeDatabaseStore(client, TEST_NODES_TABLE)

	// Set environment variables for handler
	os.Setenv("COMPUTE_NODES_TABLE", TEST_NODES_TABLE)
	// Don't override ENV if already set (Docker sets it to DOCKER)
	if os.Getenv("ENV") == "" {
		os.Setenv("ENV", "TEST") // This triggers test-aware config in LoadAWSConfig
	}
	// Don't override DYNAMODB_URL if already set (Docker sets it to http://dynamodb:8000)
	if os.Getenv("DYNAMODB_URL") == "" {
		os.Setenv("DYNAMODB_URL", "http://localhost:8000") // Local DynamoDB endpoint for local testing
	}

	return nodeStore
}

func TestPatchComputeNodeHandler_Success_UpdateAll(t *testing.T) {
	nodeStore := setupPatchComputeNodeHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create and insert test node
	testNode := createTestNode(testId)
	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	// Create update request with all fields
	newName := "Updated Name"
	newDescription := "Updated Description"
	newStatus := "Paused"
	updateRequest := compute.ComputeNodeUpdateRequest{
		Name:        &newName,
		Description: &newDescription,
		Status:      &newStatus,
	}
	requestBody, err := json.Marshal(updateRequest)
	require.NoError(t, err)

	// Create request with test authorizer - user is the owner
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testNode.Uuid,
		},
		Body: string(requestBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
		},
	}

	// Call the handler
	response, err := compute.PatchComputeNodeHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, 200, response.StatusCode)

	// Parse response body
	var nodeResponse models.Node
	err = json.Unmarshal([]byte(response.Body), &nodeResponse)
	assert.NoError(t, err)
	assert.Equal(t, newName, nodeResponse.Name)
	assert.Equal(t, newDescription, nodeResponse.Description)
	assert.Equal(t, newStatus, nodeResponse.Status)
	assert.Equal(t, testNode.Uuid, nodeResponse.Uuid)
}

func TestPatchComputeNodeHandler_Success_UpdateName(t *testing.T) {
	nodeStore := setupPatchComputeNodeHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create and insert test node
	testNode := createTestNode(testId)
	originalDescription := testNode.Description
	originalStatus := testNode.Status
	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	// Create update request with only name
	newName := "Updated Name Only"
	updateRequest := compute.ComputeNodeUpdateRequest{
		Name: &newName,
	}
	requestBody, err := json.Marshal(updateRequest)
	require.NoError(t, err)

	// Create request with test authorizer
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testNode.Uuid,
		},
		Body: string(requestBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
		},
	}

	// Call the handler
	response, err := compute.PatchComputeNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 200, response.StatusCode)

	// Parse response and verify only name changed
	var nodeResponse models.Node
	err = json.Unmarshal([]byte(response.Body), &nodeResponse)
	assert.NoError(t, err)
	assert.Equal(t, newName, nodeResponse.Name)
	assert.Equal(t, originalDescription, nodeResponse.Description)
	assert.Equal(t, originalStatus, nodeResponse.Status)
}

func TestPatchComputeNodeHandler_Success_UpdateStatus(t *testing.T) {
	nodeStore := setupPatchComputeNodeHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create and insert test node with Enabled status
	testNode := createTestNode(testId)
	testNode.Status = "Enabled"
	originalName := testNode.Name
	originalDescription := testNode.Description
	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	// Create update request to pause the node
	newStatus := "Paused"
	updateRequest := compute.ComputeNodeUpdateRequest{
		Status: &newStatus,
	}
	requestBody, err := json.Marshal(updateRequest)
	require.NoError(t, err)

	// Create request with test authorizer
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testNode.Uuid,
		},
		Body: string(requestBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
		},
	}

	// Call the handler
	response, err := compute.PatchComputeNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 200, response.StatusCode)

	// Parse response and verify only status changed
	var nodeResponse models.Node
	err = json.Unmarshal([]byte(response.Body), &nodeResponse)
	assert.NoError(t, err)
	assert.Equal(t, originalName, nodeResponse.Name)
	assert.Equal(t, originalDescription, nodeResponse.Description)
	assert.Equal(t, newStatus, nodeResponse.Status)
}

func TestPatchComputeNodeHandler_NotFound(t *testing.T) {
	_ = setupPatchComputeNodeHandlerTest(t)
	ctx := context.Background()

	nonExistentId := uuid.New().String()

	// Create update request
	newName := "Test"
	updateRequest := compute.ComputeNodeUpdateRequest{
		Name: &newName,
	}
	requestBody, err := json.Marshal(updateRequest)
	require.NoError(t, err)

	// Create request with test authorizer
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": nonExistentId,
		},
		Body: string(requestBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("user-123", "org-123"),
		},
	}

	// Call the handler
	response, err := compute.PatchComputeNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 404, response.StatusCode)
	assert.Contains(t, response.Body, "not found")
}

func TestPatchComputeNodeHandler_Forbidden_NotOwner(t *testing.T) {
	nodeStore := setupPatchComputeNodeHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create and insert test node
	testNode := createTestNode(testId)
	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	// Create update request
	newName := "Should Fail"
	updateRequest := compute.ComputeNodeUpdateRequest{
		Name: &newName,
	}
	requestBody, err := json.Marshal(updateRequest)
	require.NoError(t, err)

	// Create request with different user (not the owner)
	differentUser := "different-user-" + testId
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testNode.Uuid,
		},
		Body: string(requestBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(differentUser, testNode.OrganizationId),
		},
	}

	// Call the handler
	response, err := compute.PatchComputeNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 403, response.StatusCode)
	assert.Contains(t, response.Body, "forbidden")
}

func TestPatchComputeNodeHandler_BadRequest_InvalidStatus(t *testing.T) {
	nodeStore := setupPatchComputeNodeHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create and insert test node
	testNode := createTestNode(testId)
	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	// Create update request with invalid status
	invalidStatus := "InvalidStatus"
	updateRequest := compute.ComputeNodeUpdateRequest{
		Status: &invalidStatus,
	}
	requestBody, err := json.Marshal(updateRequest)
	require.NoError(t, err)

	// Create request with test authorizer
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testNode.Uuid,
		},
		Body: string(requestBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
		},
	}

	// Call the handler
	response, err := compute.PatchComputeNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 400, response.StatusCode)
	assert.Contains(t, response.Body, "invalid status")
}

func TestPatchComputeNodeHandler_BadRequest_NoFieldsToUpdate(t *testing.T) {
	nodeStore := setupPatchComputeNodeHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create and insert test node
	testNode := createTestNode(testId)
	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	// Create empty update request
	updateRequest := compute.ComputeNodeUpdateRequest{}
	requestBody, err := json.Marshal(updateRequest)
	require.NoError(t, err)

	// Create request with test authorizer
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testNode.Uuid,
		},
		Body: string(requestBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
		},
	}

	// Call the handler
	response, err := compute.PatchComputeNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 400, response.StatusCode)
}

func TestPatchComputeNodeHandler_BadRequest_InvalidJSON(t *testing.T) {
	_ = setupPatchComputeNodeHandlerTest(t)
	ctx := context.Background()

	// Create request with invalid JSON
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": uuid.New().String(),
		},
		Body: "invalid json",
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("user-123", "org-123"),
		},
	}

	// Call the handler
	response, err := compute.PatchComputeNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 400, response.StatusCode)
	assert.Contains(t, response.Body, "error unmarshaling")
}

func TestPatchComputeNodeHandler_BadRequest_MissingNodeId(t *testing.T) {
	_ = setupPatchComputeNodeHandlerTest(t)
	ctx := context.Background()

	// Create update request
	newName := "Test"
	updateRequest := compute.ComputeNodeUpdateRequest{
		Name: &newName,
	}
	requestBody, err := json.Marshal(updateRequest)
	require.NoError(t, err)

	// Create request without node ID
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{},
		Body:           string(requestBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("user-123", "org-123"),
		},
	}

	// Call the handler
	response, err := compute.PatchComputeNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 400, response.StatusCode)
	assert.Contains(t, response.Body, "missing node uuid")
}

func TestPatchComputeNodeHandler_PendingStatus_CannotChangeStatus(t *testing.T) {
	nodeStore := setupPatchComputeNodeHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create and insert test node with Pending status
	testNode := createTestNode(testId)
	testNode.Status = "Pending"
	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	// Try to change status to Enabled
	newStatus := "Enabled"
	updateRequest := compute.ComputeNodeUpdateRequest{
		Status: &newStatus,
	}
	requestBody, err := json.Marshal(updateRequest)
	require.NoError(t, err)

	// Create request with test authorizer (owner)
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testNode.Uuid,
		},
		Body: string(requestBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
		},
	}

	// Call the handler
	response, err := compute.PatchComputeNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 400, response.StatusCode)
	assert.Contains(t, response.Body, "cannot modify status of pending node")
}

func TestPatchComputeNodeHandler_PendingStatus_CanUpdateOtherFields(t *testing.T) {
	nodeStore := setupPatchComputeNodeHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create and insert test node with Pending status
	testNode := createTestNode(testId)
	testNode.Status = "Pending"
	originalStatus := testNode.Status
	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	// Update name and description (but not status)
	newName := "Updated Name"
	newDescription := "Updated Description"
	updateRequest := compute.ComputeNodeUpdateRequest{
		Name:        &newName,
		Description: &newDescription,
	}
	requestBody, err := json.Marshal(updateRequest)
	require.NoError(t, err)

	// Create request with test authorizer (owner)
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testNode.Uuid,
		},
		Body: string(requestBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
		},
	}

	// Call the handler - should succeed
	response, err := compute.PatchComputeNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 200, response.StatusCode)

	// Parse response and verify changes
	var nodeResponse models.Node
	err = json.Unmarshal([]byte(response.Body), &nodeResponse)
	assert.NoError(t, err)
	assert.Equal(t, newName, nodeResponse.Name)
	assert.Equal(t, newDescription, nodeResponse.Description)
	assert.Equal(t, originalStatus, nodeResponse.Status) // Status should remain Pending
}

func TestPatchComputeNodeHandler_CannotEnableNodeWhenAccountPaused(t *testing.T) {
	nodeStore := setupPatchComputeNodeHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Set up ACCOUNTS_TABLE environment variable for account status check
	os.Setenv("ACCOUNTS_TABLE", test.TEST_ACCOUNTS_WITH_INDEX_TABLE)

	// Create and insert test node with Paused status
	testNode := createTestNode(testId)
	testNode.Status = "Paused"
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

	// Try to enable the node
	newStatus := "Enabled"
	updateRequest := compute.ComputeNodeUpdateRequest{
		Status: &newStatus,
	}
	requestBody, err := json.Marshal(updateRequest)
	require.NoError(t, err)

	// Create request with test authorizer (owner)
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testNode.Uuid,
		},
		Body: string(requestBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
		},
	}

	// Call the handler
	response, err := compute.PatchComputeNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 400, response.StatusCode)
	assert.Contains(t, response.Body, "cannot enable compute node while account is paused")
}

func TestPatchComputeNodeHandler_CanPauseNodeWhenAccountPaused(t *testing.T) {
	nodeStore := setupPatchComputeNodeHandlerTest(t)
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

	// Try to pause the node (this should be allowed)
	newStatus := "Paused"
	updateRequest := compute.ComputeNodeUpdateRequest{
		Status: &newStatus,
	}
	requestBody, err := json.Marshal(updateRequest)
	require.NoError(t, err)

	// Create request with test authorizer (owner)
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testNode.Uuid,
		},
		Body: string(requestBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
		},
	}

	// Call the handler - should succeed
	response, err := compute.PatchComputeNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 200, response.StatusCode)

	// Verify the node was paused
	var nodeResponse models.Node
	err = json.Unmarshal([]byte(response.Body), &nodeResponse)
	assert.NoError(t, err)
	assert.Equal(t, "Paused", nodeResponse.Status)
}

func TestPatchComputeNodeHandler_OrganizationIndependent(t *testing.T) {
	nodeStore := setupPatchComputeNodeHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create and insert test node with INDEPENDENT organization
	testNode := createTestNode(testId)
	testNode.OrganizationId = "INDEPENDENT"
	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	// Create update request
	newName := "Updated Independent Node"
	updateRequest := compute.ComputeNodeUpdateRequest{
		Name: &newName,
	}
	requestBody, err := json.Marshal(updateRequest)
	require.NoError(t, err)

	// Create request with test authorizer
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testNode.Uuid,
		},
		Body: string(requestBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, ""),
		},
	}

	// Call the handler
	response, err := compute.PatchComputeNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 200, response.StatusCode)

	// Parse response and verify OrganizationId is returned as empty string
	var nodeResponse models.Node
	err = json.Unmarshal([]byte(response.Body), &nodeResponse)
	assert.NoError(t, err)
	assert.Equal(t, newName, nodeResponse.Name)
	assert.Equal(t, "", nodeResponse.OrganizationId) // Should be empty string, not "INDEPENDENT"
}