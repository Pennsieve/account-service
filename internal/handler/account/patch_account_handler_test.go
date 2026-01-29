package account_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	account_handler "github.com/pennsieve/account-service/internal/handler/account"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupPatchAccountHandlerTest(t *testing.T) (*store_dynamodb.AccountDatabaseStore, store_dynamodb.NodeStore, string) {
	// Generate unique test ID for isolation
	testId := test.GenerateTestId()
	
	// Use shared test client and tables
	client := test.GetClient()
	accountStore := store_dynamodb.NewAccountDatabaseStore(client, TEST_ACCOUNTS_WITH_INDEX_TABLE).(*store_dynamodb.AccountDatabaseStore)
	nodeStore := store_dynamodb.NewNodeDatabaseStore(client, TEST_NODES_TABLE)
	
	// Set environment variables for handler to use test client and test authorization
	os.Setenv("ACCOUNTS_TABLE", TEST_ACCOUNTS_WITH_INDEX_TABLE)
	os.Setenv("NODES_TABLE", TEST_NODES_TABLE)
	// Don't override ENV if already set (Docker sets it to DOCKER)
	if os.Getenv("ENV") == "" {
		os.Setenv("ENV", "TEST")  // This triggers test-aware config in LoadAWSConfig
	}
	// Don't override DYNAMODB_URL if already set (Docker sets it to http://dynamodb:8000)
	if os.Getenv("DYNAMODB_URL") == "" {
		os.Setenv("DYNAMODB_URL", "http://localhost:8000")  // Local DynamoDB endpoint for local testing
	}
	os.Setenv("TEST_USER_ID", testId)  // Set unique test user ID for authorization
	
	// Register cleanup for this test's specific data
	t.Cleanup(func() {
		// Clean up only this test's data using unique IDs
		// Note: In a real cleanup, you'd want to remove specific records
		// For now, rely on unique IDs to prevent conflicts
		os.Unsetenv("TEST_USER_ID")
	})

	return accountStore, nodeStore, testId
}

func createTestAccount(ctx context.Context, accountStore *store_dynamodb.AccountDatabaseStore, userId string, testId string) store_dynamodb.Account {
	account := store_dynamodb.Account{
		Uuid:        "patch-test-account-" + testId,
		UserId:      userId,
		AccountId:   "123456789012",
		AccountType: "aws",
		RoleName:    "test-patch-role",
		ExternalId:  "ext-patch-" + testId,
		Name:        "Original Account Name",
		Description: "Original account description",
		Status:      "Enabled",
	}
	return account
}

func TestPatchAccountHandler_Success_UpdateStatus(t *testing.T) {
	accountStore, _, testId := setupPatchAccountHandlerTest(t)
	ctx := context.Background()

	userId := testId
	
	// Create test account
	account := createTestAccount(ctx, accountStore, userId, testId)
	err := accountStore.Insert(ctx, account)
	require.NoError(t, err)

	// Create PATCH request to update status
	requestBody := `{"status": "Paused"}`
	request := events.APIGatewayV2HTTPRequest{
		Body: requestBody,
		PathParameters: map[string]string{
			"id": account.Uuid,
		},
	}

	// Call the handler
	response, err := account_handler.PatchAccountHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, 200, response.StatusCode)
	
	// Parse response body
	var updatedAccount models.Account
	err = json.Unmarshal([]byte(response.Body), &updatedAccount)
	assert.NoError(t, err)
	
	// Verify the status was updated
	assert.Equal(t, account.Uuid, updatedAccount.Uuid)
	assert.Equal(t, "Paused", updatedAccount.Status)
	assert.Equal(t, account.Name, updatedAccount.Name) // Other fields unchanged
	assert.Equal(t, account.Description, updatedAccount.Description)
}

func TestPatchAccountHandler_Success_UpdateName(t *testing.T) {
	accountStore, _, testId := setupPatchAccountHandlerTest(t)
	ctx := context.Background()

	userId := testId
	
	// Create test account
	account := createTestAccount(ctx, accountStore, userId, testId)
	err := accountStore.Insert(ctx, account)
	require.NoError(t, err)

	// Create PATCH request to update name
	requestBody := `{"name": "Updated Account Name"}`
	request := events.APIGatewayV2HTTPRequest{
		Body: requestBody,
		PathParameters: map[string]string{
			"id": account.Uuid,
		},
	}

	// Call the handler
	response, err := account_handler.PatchAccountHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, 200, response.StatusCode)
	
	// Parse response body
	var updatedAccount models.Account
	err = json.Unmarshal([]byte(response.Body), &updatedAccount)
	assert.NoError(t, err)
	
	// Verify the name was updated
	assert.Equal(t, account.Uuid, updatedAccount.Uuid)
	assert.Equal(t, "Updated Account Name", updatedAccount.Name)
	assert.Equal(t, account.Status, updatedAccount.Status) // Other fields unchanged
	assert.Equal(t, account.Description, updatedAccount.Description)
}

func TestPatchAccountHandler_Success_UpdateDescription(t *testing.T) {
	accountStore, _, testId := setupPatchAccountHandlerTest(t)
	ctx := context.Background()

	userId := testId
	
	// Create test account
	account := createTestAccount(ctx, accountStore, userId, testId)
	err := accountStore.Insert(ctx, account)
	require.NoError(t, err)

	// Create PATCH request to update description
	requestBody := `{"description": "Updated account description"}`
	request := events.APIGatewayV2HTTPRequest{
		Body: requestBody,
		PathParameters: map[string]string{
			"id": account.Uuid,
		},
	}

	// Call the handler
	response, err := account_handler.PatchAccountHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, 200, response.StatusCode)
	
	// Parse response body
	var updatedAccount models.Account
	err = json.Unmarshal([]byte(response.Body), &updatedAccount)
	assert.NoError(t, err)
	
	// Verify the description was updated
	assert.Equal(t, account.Uuid, updatedAccount.Uuid)
	assert.Equal(t, "Updated account description", updatedAccount.Description)
	assert.Equal(t, account.Status, updatedAccount.Status) // Other fields unchanged
	assert.Equal(t, account.Name, updatedAccount.Name)
}

func TestPatchAccountHandler_Success_UpdateMultipleFields(t *testing.T) {
	accountStore, _, testId := setupPatchAccountHandlerTest(t)
	ctx := context.Background()

	userId := testId
	
	// Create test account
	account := createTestAccount(ctx, accountStore, userId, testId)
	err := accountStore.Insert(ctx, account)
	require.NoError(t, err)

	// Create PATCH request to update multiple fields
	requestBody := `{"status": "Paused", "name": "Multi-Updated Name", "description": "Multi-updated description"}`
	request := events.APIGatewayV2HTTPRequest{
		Body: requestBody,
		PathParameters: map[string]string{
			"id": account.Uuid,
		},
	}

	// Call the handler
	response, err := account_handler.PatchAccountHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, 200, response.StatusCode)
	
	// Parse response body
	var updatedAccount models.Account
	err = json.Unmarshal([]byte(response.Body), &updatedAccount)
	assert.NoError(t, err)
	
	// Verify all fields were updated
	assert.Equal(t, account.Uuid, updatedAccount.Uuid)
	assert.Equal(t, "Paused", updatedAccount.Status)
	assert.Equal(t, "Multi-Updated Name", updatedAccount.Name)
	assert.Equal(t, "Multi-updated description", updatedAccount.Description)
}

func TestPatchAccountHandler_Success_PauseWithComputeNodes(t *testing.T) {
	accountStore, nodeStore, testId := setupPatchAccountHandlerTest(t)
	ctx := context.Background()

	userId := testId
	
	// Create test account
	account := createTestAccount(ctx, accountStore, userId, testId)
	err := accountStore.Insert(ctx, account)
	require.NoError(t, err)

	// Create test compute nodes for this account
	testNodes := []models.DynamoDBNode{
		{
			Uuid:           "node-1-" + testId,
			AccountUuid:    account.Uuid,
			UserId:         userId,
			OrganizationId: "org-" + testId,
			Name:           "Test Node 1",
			Status:         "Enabled",
		},
		{
			Uuid:           "node-2-" + testId,
			AccountUuid:    account.Uuid,
			UserId:         userId,
			OrganizationId: "org-" + testId,
			Name:           "Test Node 2",
			Status:         "Enabled",
		},
	}

	for _, node := range testNodes {
		err := nodeStore.Put(ctx, node)
		require.NoError(t, err)
	}

	// Create PATCH request to pause account (should pause all nodes)
	requestBody := `{"status": "Paused"}`
	request := events.APIGatewayV2HTTPRequest{
		Body: requestBody,
		PathParameters: map[string]string{
			"id": account.Uuid,
		},
	}

	// Call the handler
	response, err := account_handler.PatchAccountHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, 200, response.StatusCode)
	
	// Parse response body
	var updatedAccount models.Account
	err = json.Unmarshal([]byte(response.Body), &updatedAccount)
	assert.NoError(t, err)
	assert.Equal(t, "Paused", updatedAccount.Status)

	// Verify all compute nodes were paused
	for _, testNode := range testNodes {
		node, err := nodeStore.GetById(ctx, testNode.Uuid)
		assert.NoError(t, err)
		assert.Equal(t, "Paused", node.Status, "Node %s should be paused", testNode.Uuid)
	}
}

func TestPatchAccountHandler_Error_MissingAccountId(t *testing.T) {
	_, _, _ = setupPatchAccountHandlerTest(t)
	ctx := context.Background()

	// Create PATCH request without account ID in path
	requestBody := `{"status": "Paused"}`
	request := events.APIGatewayV2HTTPRequest{
		Body: requestBody,
		// PathParameters intentionally omitted
	}

	// Call the handler
	response, err := account_handler.PatchAccountHandler(ctx, request)
	assert.NoError(t, err)

	// Verify 400 response
	assert.Equal(t, 400, response.StatusCode)
	assert.Contains(t, response.Body, "PatchAccountHandler")
}

func TestPatchAccountHandler_Error_InvalidJSON(t *testing.T) {
	accountStore, _, testId := setupPatchAccountHandlerTest(t)
	ctx := context.Background()

	userId := testId
	
	// Create test account
	account := createTestAccount(ctx, accountStore, userId, testId)
	err := accountStore.Insert(ctx, account)
	require.NoError(t, err)

	// Create PATCH request with invalid JSON
	requestBody := `{"status": "Paused", invalid json}`
	request := events.APIGatewayV2HTTPRequest{
		Body: requestBody,
		PathParameters: map[string]string{
			"id": account.Uuid,
		},
	}

	// Call the handler
	response, err := account_handler.PatchAccountHandler(ctx, request)
	assert.NoError(t, err)

	// Verify 400 response
	assert.Equal(t, 400, response.StatusCode)
	assert.Contains(t, response.Body, "error unmarshaling")
}

func TestPatchAccountHandler_Error_InvalidStatus(t *testing.T) {
	accountStore, _, testId := setupPatchAccountHandlerTest(t)
	ctx := context.Background()

	userId := testId
	
	// Create test account
	account := createTestAccount(ctx, accountStore, userId, testId)
	err := accountStore.Insert(ctx, account)
	require.NoError(t, err)

	// Create PATCH request with invalid status value
	requestBody := `{"status": "InvalidStatus"}`
	request := events.APIGatewayV2HTTPRequest{
		Body: requestBody,
		PathParameters: map[string]string{
			"id": account.Uuid,
		},
	}

	// Call the handler
	response, err := account_handler.PatchAccountHandler(ctx, request)
	assert.NoError(t, err)

	// Verify 400 response
	assert.Equal(t, 400, response.StatusCode)
	assert.Contains(t, response.Body, "invalid status")
}

func TestPatchAccountHandler_Error_NoFieldsProvided(t *testing.T) {
	accountStore, _, testId := setupPatchAccountHandlerTest(t)
	ctx := context.Background()

	userId := testId
	
	// Create test account
	account := createTestAccount(ctx, accountStore, userId, testId)
	err := accountStore.Insert(ctx, account)
	require.NoError(t, err)

	// Create PATCH request with empty JSON (no fields to update)
	requestBody := `{}`
	request := events.APIGatewayV2HTTPRequest{
		Body: requestBody,
		PathParameters: map[string]string{
			"id": account.Uuid,
		},
	}

	// Call the handler
	response, err := account_handler.PatchAccountHandler(ctx, request)
	assert.NoError(t, err)

	// Verify 400 response
	assert.Equal(t, 400, response.StatusCode)
	assert.Contains(t, response.Body, "error unmarshaling")
}

func TestPatchAccountHandler_Error_AccountNotFound(t *testing.T) {
	_, _, testId := setupPatchAccountHandlerTest(t)
	ctx := context.Background()

	// Create PATCH request for non-existent account
	requestBody := `{"status": "Paused"}`
	request := events.APIGatewayV2HTTPRequest{
		Body: requestBody,
		PathParameters: map[string]string{
			"id": "non-existent-account-" + testId,
		},
	}

	// Call the handler
	response, err := account_handler.PatchAccountHandler(ctx, request)
	assert.NoError(t, err)

	// Verify 404 response
	assert.Equal(t, 404, response.StatusCode)
	assert.Contains(t, response.Body, "not found")
}

func TestPatchAccountHandler_Error_NotAccountOwner(t *testing.T) {
	accountStore, _, testId := setupPatchAccountHandlerTest(t)
	ctx := context.Background()

	// Create account owned by a different user
	account := createTestAccount(ctx, accountStore, "different-user-"+testId, testId)
	err := accountStore.Insert(ctx, account)
	require.NoError(t, err)

	// Create PATCH request (user ID will be testId, but account is owned by different-user)
	requestBody := `{"status": "Paused"}`
	request := events.APIGatewayV2HTTPRequest{
		Body: requestBody,
		PathParameters: map[string]string{
			"id": account.Uuid,
		},
	}

	// Call the handler
	response, err := account_handler.PatchAccountHandler(ctx, request)
	assert.NoError(t, err)

	// Verify 403 response
	assert.Equal(t, 403, response.StatusCode)
	assert.Contains(t, response.Body, "does not belong to user")
}

func TestPatchAccountHandler_Success_EnableAccount_DoesNotEnableNodes(t *testing.T) {
	accountStore, nodeStore, testId := setupPatchAccountHandlerTest(t)
	ctx := context.Background()

	userId := testId
	
	// Create test account initially paused
	account := createTestAccount(ctx, accountStore, userId, testId)
	account.Status = "Paused"
	err := accountStore.Insert(ctx, account)
	require.NoError(t, err)

	// Create test compute node that's also paused
	testNode := models.DynamoDBNode{
		Uuid:           "node-paused-" + testId,
		AccountUuid:    account.Uuid,
		UserId:         userId,
		OrganizationId: "org-" + testId,
		Name:           "Paused Node",
		Status:         "Paused",
	}
	err = nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	// Create PATCH request to enable account
	requestBody := `{"status": "Enabled"}`
	request := events.APIGatewayV2HTTPRequest{
		Body: requestBody,
		PathParameters: map[string]string{
			"id": account.Uuid,
		},
	}

	// Call the handler
	response, err := account_handler.PatchAccountHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, 200, response.StatusCode)
	
	// Parse response body
	var updatedAccount models.Account
	err = json.Unmarshal([]byte(response.Body), &updatedAccount)
	assert.NoError(t, err)
	assert.Equal(t, "Enabled", updatedAccount.Status)

	// Verify compute node remains paused (per requirement)
	node, err := nodeStore.GetById(ctx, testNode.Uuid)
	assert.NoError(t, err)
	assert.Equal(t, "Paused", node.Status, "Node should remain paused when account is enabled")
}