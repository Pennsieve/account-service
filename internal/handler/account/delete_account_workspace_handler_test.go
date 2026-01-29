package account_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	account_handler "github.com/pennsieve/account-service/internal/handler/account"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupDeleteAccountWorkspaceHandlerTest(t *testing.T) (*store_dynamodb.AccountDatabaseStore, *store_dynamodb.AccountWorkspaceStoreImpl, string, string) {
	// Generate unique test IDs for isolation
	testId := test.GenerateTestId()
	workspaceId := "workspace-" + testId
	
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
	
	// Register cleanup for this test's specific data
	t.Cleanup(func() {
		// Clean up only this test's data using unique IDs
		os.Unsetenv("TEST_USER_ID")
	})

	return accountStore, workspaceStore, testId, workspaceId
}

func createTestAccountWithEnablement(ctx context.Context, accountStore *store_dynamodb.AccountDatabaseStore, workspaceStore *store_dynamodb.AccountWorkspaceStoreImpl, userId string, workspaceId string, testId string, isPublic bool) (store_dynamodb.Account, store_dynamodb.AccountWorkspace) {
	account := store_dynamodb.Account{
		Uuid:        "delete-test-account-" + testId,
		UserId:      userId,
		AccountId:   "123456789012",
		AccountType: "aws",
		RoleName:    "test-delete-role",
		ExternalId:  "ext-delete-" + testId,
		Name:        "Delete Test Account",
		Description: "Account for delete workspace testing",
		Status:      "Enabled",
	}
	
	enablement := store_dynamodb.AccountWorkspace{
		AccountUuid: account.Uuid,
		WorkspaceId: workspaceId,
		IsPublic:    isPublic,
		EnabledBy:   userId,
		EnabledAt:   time.Now().Unix(),
	}
	
	return account, enablement
}

func TestDeleteAccountWorkspaceHandler_Success(t *testing.T) {
	accountStore, workspaceStore, testId, workspaceId := setupDeleteAccountWorkspaceHandlerTest(t)
	ctx := context.Background()

	userId := testId
	
	// Create test account and enablement
	account, enablement := createTestAccountWithEnablement(ctx, accountStore, workspaceStore, userId, workspaceId, testId, true)
	err := accountStore.Insert(ctx, account)
	require.NoError(t, err)
	err = workspaceStore.Insert(ctx, enablement)
	require.NoError(t, err)

	// Verify enablement exists before deletion
	existingEnablement, err := workspaceStore.Get(ctx, account.Uuid, workspaceId)
	require.NoError(t, err)
	assert.Equal(t, account.Uuid, existingEnablement.AccountUuid)

	// Create DELETE request
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"uuid":        account.Uuid,
			"workspaceId": workspaceId,
		},
	}

	// Call the handler
	response, err := account_handler.DeleteAccountWorkspaceEnablementHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response - should return 204 No Content
	assert.Equal(t, 204, response.StatusCode)
	assert.Empty(t, response.Body)
	
	// Verify enablement was deleted from database
	deletedEnablement, err := workspaceStore.Get(ctx, account.Uuid, workspaceId)
	assert.NoError(t, err)
	assert.Empty(t, deletedEnablement.AccountUuid, "Enablement should be deleted")
}

func TestDeleteAccountWorkspaceHandler_Error_MissingAccountUuid(t *testing.T) {
	_, _, _, workspaceId := setupDeleteAccountWorkspaceHandlerTest(t)
	ctx := context.Background()

	// Create DELETE request without account UUID
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"workspaceId": workspaceId,
		},
	}

	// Call the handler
	response, err := account_handler.DeleteAccountWorkspaceEnablementHandler(ctx, request)
	assert.NoError(t, err)

	// Verify 400 response
	assert.Equal(t, 400, response.StatusCode)
	assert.Contains(t, response.Body, "DeleteAccountWorkspaceEnablementHandler")
}

func TestDeleteAccountWorkspaceHandler_Error_MissingWorkspaceId(t *testing.T) {
	accountStore, _, testId, _ := setupDeleteAccountWorkspaceHandlerTest(t)
	ctx := context.Background()

	userId := testId
	
	// Create test account
	account := store_dynamodb.Account{
		Uuid:        "delete-test-account-" + testId,
		UserId:      userId,
		AccountId:   "123456789012",
		AccountType: "aws",
		RoleName:    "test-role",
		ExternalId:  "ext-" + testId,
		Name:        "Test Account",
		Description: "Test account",
		Status:      "Enabled",
	}
	err := accountStore.Insert(ctx, account)
	require.NoError(t, err)

	// Create DELETE request without workspace ID
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"uuid": account.Uuid,
		},
	}

	// Call the handler
	response, err := account_handler.DeleteAccountWorkspaceEnablementHandler(ctx, request)
	assert.NoError(t, err)

	// Verify 400 response
	assert.Equal(t, 400, response.StatusCode)
	assert.Contains(t, response.Body, "missing workspace")
}

func TestDeleteAccountWorkspaceHandler_Error_AccountNotFound(t *testing.T) {
	_, _, testId, workspaceId := setupDeleteAccountWorkspaceHandlerTest(t)
	ctx := context.Background()

	// Create DELETE request for non-existent account
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"uuid":        "non-existent-account-" + testId,
			"workspaceId": workspaceId,
		},
	}

	// Call the handler
	response, err := account_handler.DeleteAccountWorkspaceEnablementHandler(ctx, request)
	assert.NoError(t, err)

	// Verify 404 response
	assert.Equal(t, 404, response.StatusCode)
	assert.Contains(t, response.Body, "account not found")
}

func TestDeleteAccountWorkspaceHandler_Error_NotAccountOwner(t *testing.T) {
	accountStore, workspaceStore, testId, workspaceId := setupDeleteAccountWorkspaceHandlerTest(t)
	ctx := context.Background()

	// Create account owned by a different user
	differentUserId := "different-user-" + testId
	account, enablement := createTestAccountWithEnablement(ctx, accountStore, workspaceStore, differentUserId, workspaceId, testId, true)
	err := accountStore.Insert(ctx, account)
	require.NoError(t, err)
	err = workspaceStore.Insert(ctx, enablement)
	require.NoError(t, err)

	// Create DELETE request (user ID will be testId, but account is owned by different-user)
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"uuid":        account.Uuid,
			"workspaceId": workspaceId,
		},
	}

	// Call the handler
	response, err := account_handler.DeleteAccountWorkspaceEnablementHandler(ctx, request)
	assert.NoError(t, err)

	// Verify 403 response
	assert.Equal(t, 403, response.StatusCode)
	assert.Contains(t, response.Body, "does not belong to user")
	
	// Verify enablement was NOT deleted
	stillExists, err := workspaceStore.Get(ctx, account.Uuid, workspaceId)
	assert.NoError(t, err)
	assert.Equal(t, account.Uuid, stillExists.AccountUuid, "Enablement should not be deleted")
}

func TestDeleteAccountWorkspaceHandler_Error_EnablementNotFound(t *testing.T) {
	accountStore, _, testId, workspaceId := setupDeleteAccountWorkspaceHandlerTest(t)
	ctx := context.Background()

	userId := testId
	
	// Create test account WITHOUT enablement
	account := store_dynamodb.Account{
		Uuid:        "delete-test-account-" + testId,
		UserId:      userId,
		AccountId:   "123456789012",
		AccountType: "aws",
		RoleName:    "test-role",
		ExternalId:  "ext-" + testId,
		Name:        "Test Account",
		Description: "Account without enablement",
		Status:      "Enabled",
	}
	err := accountStore.Insert(ctx, account)
	require.NoError(t, err)

	// Create DELETE request for non-existent enablement
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"uuid":        account.Uuid,
			"workspaceId": workspaceId,
		},
	}

	// Call the handler
	response, err := account_handler.DeleteAccountWorkspaceEnablementHandler(ctx, request)
	assert.NoError(t, err)

	// Verify 404 response
	assert.Equal(t, 404, response.StatusCode)
	assert.Contains(t, response.Body, "workspace enablement not found")
}

func TestDeleteAccountWorkspaceHandler_Success_DeleteSpecificWorkspace(t *testing.T) {
	accountStore, workspaceStore, testId, _ := setupDeleteAccountWorkspaceHandlerTest(t)
	ctx := context.Background()

	userId := testId
	workspace1 := "workspace-1-" + testId
	workspace2 := "workspace-2-" + testId
	
	// Create test account
	account := store_dynamodb.Account{
		Uuid:        "delete-multi-test-" + testId,
		UserId:      userId,
		AccountId:   "123456789012",
		AccountType: "aws",
		RoleName:    "test-role",
		ExternalId:  "ext-" + testId,
		Name:        "Multi Workspace Account",
		Description: "Account with multiple workspaces",
		Status:      "Enabled",
	}
	err := accountStore.Insert(ctx, account)
	require.NoError(t, err)
	
	// Create two workspace enablements
	enablement1 := store_dynamodb.AccountWorkspace{
		AccountUuid: account.Uuid,
		WorkspaceId: workspace1,
		IsPublic:    true,
		EnabledBy:   userId,
		EnabledAt:   time.Now().Unix(),
	}
	enablement2 := store_dynamodb.AccountWorkspace{
		AccountUuid: account.Uuid,
		WorkspaceId: workspace2,
		IsPublic:    false,
		EnabledBy:   userId,
		EnabledAt:   time.Now().Unix(),
	}
	
	err = workspaceStore.Insert(ctx, enablement1)
	require.NoError(t, err)
	err = workspaceStore.Insert(ctx, enablement2)
	require.NoError(t, err)

	// Delete only workspace1
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"uuid":        account.Uuid,
			"workspaceId": workspace1,
		},
	}

	response, err := account_handler.DeleteAccountWorkspaceEnablementHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 204, response.StatusCode)
	
	// Verify workspace1 was deleted
	deleted, err := workspaceStore.Get(ctx, account.Uuid, workspace1)
	assert.NoError(t, err)
	assert.Empty(t, deleted.AccountUuid, "Workspace1 enablement should be deleted")
	
	// Verify workspace2 still exists
	stillExists, err := workspaceStore.Get(ctx, account.Uuid, workspace2)
	assert.NoError(t, err)
	assert.Equal(t, account.Uuid, stillExists.AccountUuid, "Workspace2 enablement should still exist")
	assert.Equal(t, workspace2, stillExists.WorkspaceId)
}

func TestDeleteAccountWorkspaceHandler_Success_IdempotentDelete(t *testing.T) {
	accountStore, workspaceStore, testId, workspaceId := setupDeleteAccountWorkspaceHandlerTest(t)
	ctx := context.Background()

	userId := testId
	
	// Create test account and enablement
	account, enablement := createTestAccountWithEnablement(ctx, accountStore, workspaceStore, userId, workspaceId, testId, true)
	err := accountStore.Insert(ctx, account)
	require.NoError(t, err)
	err = workspaceStore.Insert(ctx, enablement)
	require.NoError(t, err)

	// Create DELETE request
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"uuid":        account.Uuid,
			"workspaceId": workspaceId,
		},
	}

	// First deletion
	response, err := account_handler.DeleteAccountWorkspaceEnablementHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 204, response.StatusCode)

	// Attempt second deletion of same enablement
	// This should return 404 since the enablement no longer exists
	response2, err := account_handler.DeleteAccountWorkspaceEnablementHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 404, response2.StatusCode)
	assert.Contains(t, response2.Body, "workspace enablement not found")
}