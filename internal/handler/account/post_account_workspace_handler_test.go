package account_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	account_handler "github.com/pennsieve/account-service/internal/handler/account"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupPostAccountWorkspaceHandlerTest(t *testing.T) (*store_dynamodb.AccountDatabaseStore, *store_dynamodb.AccountWorkspaceStoreImpl, string, string) {
	// Generate unique test IDs for isolation
	testId := test.GenerateTestId()
	organizationId := "org-" + testId
	
	// Use shared test client and tables
	client := test.GetClient()
	accountStore := store_dynamodb.NewAccountDatabaseStore(client, TEST_ACCOUNTS_WITH_INDEX_TABLE).(*store_dynamodb.AccountDatabaseStore)
	workspaceStore := store_dynamodb.NewAccountWorkspaceStore(client, TEST_WORKSPACE_TABLE).(*store_dynamodb.AccountWorkspaceStoreImpl)
	
	// Set environment variables for handler to use test client and test authorization
	os.Setenv("ACCOUNTS_TABLE", TEST_ACCOUNTS_WITH_INDEX_TABLE)
	os.Setenv("ACCOUNT_WORKSPACE_TABLE", TEST_WORKSPACE_TABLE)
	// Don't override ENV if already set (Docker sets it to DOCKER)
	if os.Getenv("ENV") == "" {
		os.Setenv("ENV", "TEST")  // This triggers test-aware config in LoadAWSConfig
	}
	// Don't override DYNAMODB_URL if already set (Docker sets it to http://dynamodb:8000)
	if os.Getenv("DYNAMODB_URL") == "" {
		os.Setenv("DYNAMODB_URL", "http://localhost:8000")  // Local DynamoDB endpoint for local testing
	}
	os.Setenv("TEST_USER_ID", testId)  // Set unique test user ID for authorization
	os.Setenv("TEST_ORGANIZATION_ID", organizationId)  // Set unique test organization ID
	
	// Register cleanup for this test's specific data
	t.Cleanup(func() {
		// Clean up only this test's data using unique IDs
		os.Unsetenv("TEST_USER_ID")
		os.Unsetenv("TEST_ORGANIZATION_ID")
	})

	return accountStore, workspaceStore, testId, organizationId
}

func createTestAccountForWorkspace(ctx context.Context, accountStore *store_dynamodb.AccountDatabaseStore, userId string, testId string) store_dynamodb.Account {
	account := store_dynamodb.Account{
		Uuid:        "workspace-test-account-" + testId,
		UserId:      userId,
		AccountId:   "123456789012",
		AccountType: "aws",
		RoleName:    "test-workspace-role",
		ExternalId:  "ext-workspace-" + testId,
		Name:        "Workspace Test Account",
		Description: "Account for workspace enablement testing",
		Status:      "Enabled",
	}
	return account
}

func TestPostAccountWorkspaceHandler_Success_PublicEnablement(t *testing.T) {
	accountStore, workspaceStore, testId, organizationId := setupPostAccountWorkspaceHandlerTest(t)
	ctx := context.Background()

	userId := testId
	
	// Create test account
	account := createTestAccountForWorkspace(ctx, accountStore, userId, testId)
	err := accountStore.Insert(ctx, account)
	require.NoError(t, err)

	// Create POST request to enable workspace with public access
	requestBody := `{"isPublic": true}`
	request := events.APIGatewayV2HTTPRequest{
		Body: requestBody,
		PathParameters: map[string]string{
			"uuid": account.Uuid,
		},
	}

	// Call the handler
	response, err := account_handler.PostAccountWorkspaceEnablementHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, 201, response.StatusCode)
	
	// Parse response body
	var enablement models.AccountWorkspaceEnablement
	err = json.Unmarshal([]byte(response.Body), &enablement)
	assert.NoError(t, err)
	
	// Verify the enablement was created correctly
	assert.Equal(t, account.Uuid, enablement.AccountUuid)
	assert.Equal(t, organizationId, enablement.OrganizationId)
	assert.True(t, enablement.IsPublic)
	assert.Equal(t, userId, enablement.EnabledBy)
	assert.Greater(t, enablement.EnabledAt, int64(0))
	
	// Verify it was stored in the database
	storedEnablement, err := workspaceStore.Get(ctx, account.Uuid, organizationId)
	assert.NoError(t, err)
	assert.Equal(t, account.Uuid, storedEnablement.AccountUuid)
	assert.Equal(t, organizationId, storedEnablement.WorkspaceId)
	assert.True(t, storedEnablement.IsPublic)
}

func TestPostAccountWorkspaceHandler_Success_PrivateEnablement(t *testing.T) {
	accountStore, workspaceStore, testId, organizationId := setupPostAccountWorkspaceHandlerTest(t)
	ctx := context.Background()

	userId := testId
	
	// Create test account
	account := createTestAccountForWorkspace(ctx, accountStore, userId, testId)
	err := accountStore.Insert(ctx, account)
	require.NoError(t, err)

	// Create POST request to enable workspace with private access (only account owner)
	requestBody := `{"isPublic": false}`
	request := events.APIGatewayV2HTTPRequest{
		Body: requestBody,
		PathParameters: map[string]string{
			"uuid": account.Uuid,
		},
	}

	// Call the handler
	response, err := account_handler.PostAccountWorkspaceEnablementHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, 201, response.StatusCode)
	
	// Parse response body
	var enablement models.AccountWorkspaceEnablement
	err = json.Unmarshal([]byte(response.Body), &enablement)
	assert.NoError(t, err)
	
	// Verify the enablement was created with isPublic=false
	assert.Equal(t, account.Uuid, enablement.AccountUuid)
	assert.Equal(t, organizationId, enablement.OrganizationId)
	assert.False(t, enablement.IsPublic)
	assert.Equal(t, userId, enablement.EnabledBy)
	
	// Verify it was stored in the database
	storedEnablement, err := workspaceStore.Get(ctx, account.Uuid, organizationId)
	assert.NoError(t, err)
	assert.False(t, storedEnablement.IsPublic)
}

func TestPostAccountWorkspaceHandler_Error_MissingAccountUuid(t *testing.T) {
	_, _, _, _ = setupPostAccountWorkspaceHandlerTest(t)
	ctx := context.Background()

	// Create POST request without account UUID in path
	requestBody := `{"isPublic": true}`
	request := events.APIGatewayV2HTTPRequest{
		Body: requestBody,
		// PathParameters intentionally omitted
	}

	// Call the handler
	response, err := account_handler.PostAccountWorkspaceEnablementHandler(ctx, request)
	assert.NoError(t, err)

	// Verify 400 response
	assert.Equal(t, 400, response.StatusCode)
	assert.Contains(t, response.Body, "PostAccountWorkspaceEnablementHandler")
}

func TestPostAccountWorkspaceHandler_Error_InvalidJSON(t *testing.T) {
	accountStore, _, testId, _ := setupPostAccountWorkspaceHandlerTest(t)
	ctx := context.Background()

	userId := testId
	
	// Create test account
	account := createTestAccountForWorkspace(ctx, accountStore, userId, testId)
	err := accountStore.Insert(ctx, account)
	require.NoError(t, err)

	// Create POST request with invalid JSON
	requestBody := `{"isPublic": true, invalid json}`
	request := events.APIGatewayV2HTTPRequest{
		Body: requestBody,
		PathParameters: map[string]string{
			"uuid": account.Uuid,
		},
	}

	// Call the handler
	response, err := account_handler.PostAccountWorkspaceEnablementHandler(ctx, request)
	assert.NoError(t, err)

	// Verify 400 response for bad JSON
	assert.Equal(t, 400, response.StatusCode)
	assert.Contains(t, response.Body, "error unmarshaling")
}

func TestPostAccountWorkspaceHandler_Error_AccountNotFound(t *testing.T) {
	_, _, testId, _ := setupPostAccountWorkspaceHandlerTest(t)
	ctx := context.Background()

	// Create POST request for non-existent account
	requestBody := `{"isPublic": true}`
	request := events.APIGatewayV2HTTPRequest{
		Body: requestBody,
		PathParameters: map[string]string{
			"uuid": "non-existent-account-" + testId,
		},
	}

	// Call the handler
	response, err := account_handler.PostAccountWorkspaceEnablementHandler(ctx, request)
	assert.NoError(t, err)

	// Verify 404 response
	assert.Equal(t, 404, response.StatusCode)
	assert.Contains(t, response.Body, "account not found")
}

func TestPostAccountWorkspaceHandler_Error_NotAccountOwner(t *testing.T) {
	accountStore, _, testId, _ := setupPostAccountWorkspaceHandlerTest(t)
	ctx := context.Background()

	// Create account owned by a different user
	account := createTestAccountForWorkspace(ctx, accountStore, "different-user-"+testId, testId)
	err := accountStore.Insert(ctx, account)
	require.NoError(t, err)

	// Create POST request (user ID will be testId, but account is owned by different-user)
	requestBody := `{"isPublic": true}`
	request := events.APIGatewayV2HTTPRequest{
		Body: requestBody,
		PathParameters: map[string]string{
			"uuid": account.Uuid,
		},
	}

	// Call the handler
	response, err := account_handler.PostAccountWorkspaceEnablementHandler(ctx, request)
	assert.NoError(t, err)

	// Verify 403 response
	assert.Equal(t, 403, response.StatusCode)
	assert.Contains(t, response.Body, "does not belong to user")
}

func TestPostAccountWorkspaceHandler_Error_DuplicateEnablement(t *testing.T) {
	accountStore, workspaceStore, testId, organizationId := setupPostAccountWorkspaceHandlerTest(t)
	ctx := context.Background()

	userId := testId
	
	// Create test account
	account := createTestAccountForWorkspace(ctx, accountStore, userId, testId)
	err := accountStore.Insert(ctx, account)
	require.NoError(t, err)

	// Create existing enablement
	existingEnablement := store_dynamodb.AccountWorkspace{
		AccountUuid: account.Uuid,
		WorkspaceId: organizationId,
		IsPublic:    true,
		EnabledBy:   userId,
		EnabledAt:   time.Now().Unix(),
	}
	err = workspaceStore.Insert(ctx, existingEnablement)
	require.NoError(t, err)

	// Try to create duplicate enablement
	requestBody := `{"isPublic": false}`
	request := events.APIGatewayV2HTTPRequest{
		Body: requestBody,
		PathParameters: map[string]string{
			"uuid": account.Uuid,
		},
	}

	// Call the handler
	response, err := account_handler.PostAccountWorkspaceEnablementHandler(ctx, request)
	assert.NoError(t, err)

	// Verify 422 response (Unprocessable Entity)
	assert.Equal(t, 422, response.StatusCode)
	assert.Contains(t, response.Body, "already enabled for workspace")
}

func TestPostAccountWorkspaceHandler_Success_MultipleAccountsSameWorkspace(t *testing.T) {
	accountStore, workspaceStore, testId, organizationId := setupPostAccountWorkspaceHandlerTest(t)
	ctx := context.Background()

	userId := testId
	
	// Create two test accounts
	account1 := store_dynamodb.Account{
		Uuid:        "workspace-test-account-1-" + testId,
		UserId:      userId,
		AccountId:   "123456789012",
		AccountType: "aws",
		RoleName:    "test-role-1",
		ExternalId:  "ext-1-" + testId,
		Name:        "Account 1",
		Description: "First account",
		Status:      "Enabled",
	}
	account2 := store_dynamodb.Account{
		Uuid:        "workspace-test-account-2-" + testId,
		UserId:      userId,
		AccountId:   "123456789013",
		AccountType: "aws",
		RoleName:    "test-role-2",
		ExternalId:  "ext-2-" + testId,
		Name:        "Account 2",
		Description: "Second account",
		Status:      "Enabled",
	}
	
	err := accountStore.Insert(ctx, account1)
	require.NoError(t, err)
	err = accountStore.Insert(ctx, account2)
	require.NoError(t, err)

	// Enable first account for workspace
	requestBody1 := `{"isPublic": true}`
	request1 := events.APIGatewayV2HTTPRequest{
		Body: requestBody1,
		PathParameters: map[string]string{
			"uuid": account1.Uuid,
		},
	}
	response1, err := account_handler.PostAccountWorkspaceEnablementHandler(ctx, request1)
	assert.NoError(t, err)
	assert.Equal(t, 201, response1.StatusCode)

	// Enable second account for the same workspace
	requestBody2 := `{"isPublic": false}`
	request2 := events.APIGatewayV2HTTPRequest{
		Body: requestBody2,
		PathParameters: map[string]string{
			"uuid": account2.Uuid,
		},
	}
	response2, err := account_handler.PostAccountWorkspaceEnablementHandler(ctx, request2)
	assert.NoError(t, err)
	assert.Equal(t, 201, response2.StatusCode)

	// Verify both enablements exist with different settings
	enablement1, err := workspaceStore.Get(ctx, account1.Uuid, organizationId)
	assert.NoError(t, err)
	assert.True(t, enablement1.IsPublic)

	enablement2, err := workspaceStore.Get(ctx, account2.Uuid, organizationId)
	assert.NoError(t, err)
	assert.False(t, enablement2.IsPublic)
}

func TestPostAccountWorkspaceHandler_Success_EnabledAtTimestamp(t *testing.T) {
	accountStore, _, testId, _ := setupPostAccountWorkspaceHandlerTest(t)
	ctx := context.Background()

	userId := testId
	
	// Create test account
	account := createTestAccountForWorkspace(ctx, accountStore, userId, testId)
	err := accountStore.Insert(ctx, account)
	require.NoError(t, err)

	// Record time before request
	timeBefore := time.Now().Unix()

	// Create POST request
	requestBody := `{"isPublic": true}`
	request := events.APIGatewayV2HTTPRequest{
		Body: requestBody,
		PathParameters: map[string]string{
			"uuid": account.Uuid,
		},
	}

	// Call the handler
	response, err := account_handler.PostAccountWorkspaceEnablementHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 201, response.StatusCode)

	// Record time after request
	timeAfter := time.Now().Unix()

	// Parse response body
	var enablement models.AccountWorkspaceEnablement
	err = json.Unmarshal([]byte(response.Body), &enablement)
	assert.NoError(t, err)
	
	// Verify EnabledAt is within the expected range
	assert.GreaterOrEqual(t, enablement.EnabledAt, timeBefore)
	assert.LessOrEqual(t, enablement.EnabledAt, timeAfter)
}