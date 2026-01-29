package account_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/account-service/internal/handler/account"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupPostAccountHandlerTest(t *testing.T) (*store_dynamodb.AccountDatabaseStore, string) {
	// Generate unique test ID for isolation
	testId := test.GenerateTestId()
	
	// Use shared test client and accounts table with index
	client := test.GetClient()
	store := store_dynamodb.NewAccountDatabaseStore(client, TEST_ACCOUNTS_WITH_INDEX_TABLE).(*store_dynamodb.AccountDatabaseStore)
	
	// Set environment variables for handler to use test client and test authorization
	os.Setenv("ACCOUNTS_TABLE", TEST_ACCOUNTS_WITH_INDEX_TABLE)
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

	return store, testId
}

// No need for complex authorization context in test mode
// Authorization is handled via TEST_USER_ID environment variable

func TestPostAccountsHandler_Success(t *testing.T) {
	store, testId := setupPostAccountHandlerTest(t)
	ctx := context.Background()

	// In test mode, userId comes from TEST_USER_ID environment variable (set to testId)
	userId := testId
	accountData := models.Account{
		AccountId:   "123456789012-" + testId,
		AccountType: "aws",
		RoleName:    "test-role-" + testId,
		ExternalId:  "ext-123-" + testId,
		Name:        "Test Account " + testId,
		Description: "Test account for handler testing",
	}

	requestBody, err := json.Marshal(accountData)
	require.NoError(t, err)

	// Create simple request event (authorization handled via ENV=TEST)
	request := events.APIGatewayV2HTTPRequest{
		Body: string(requestBody),
	}

	// Call the handler
	response, err := account.PostAccountsHandler(ctx, request)
	assert.NoError(t, err)
	
	// Debug: Print the response if it's not the expected status
	if response.StatusCode != 201 {
		t.Logf("Unexpected response status: %d, body: %s", response.StatusCode, response.Body)
	}

	// Verify response
	assert.Equal(t, 201, response.StatusCode)

	// Parse response body to get the created account UUID
	var accountResponse models.AccountResponse
	err = json.Unmarshal([]byte(response.Body), &accountResponse)
	assert.NoError(t, err)
	assert.NotEmpty(t, accountResponse.Uuid)

	// Verify the account was actually stored
	storedAccount, err := store.GetById(ctx, accountResponse.Uuid)
	require.NoError(t, err)
	assert.Equal(t, accountResponse.Uuid, storedAccount.Uuid)
	assert.Equal(t, userId, storedAccount.UserId)
	assert.Equal(t, accountData.AccountId, storedAccount.AccountId)
	assert.Equal(t, accountData.AccountType, storedAccount.AccountType)
	assert.Equal(t, accountData.RoleName, storedAccount.RoleName)
	assert.Equal(t, accountData.ExternalId, storedAccount.ExternalId)
	assert.Equal(t, accountData.Name, storedAccount.Name)
	assert.Equal(t, accountData.Description, storedAccount.Description)
	assert.Equal(t, "Enabled", storedAccount.Status) // Default status
}

func TestPostAccountsHandler_DuplicateAccount(t *testing.T) {
	store, testId := setupPostAccountHandlerTest(t)
	ctx := context.Background()

	// In test mode, userId will be the testId from TEST_USER_ID environment variable
	// But for the duplicate test, we need to use the same testId as the existing record
	userId := testId
	accountId := "duplicate-account-" + testId

	// Insert existing account for the same user
	existingAccount := store_dynamodb.Account{
		Uuid:        "existing-uuid-" + testId,
		UserId:      userId,
		AccountId:   accountId, // Same accountId
		AccountType: "aws",
		RoleName:    "existing-role",
		ExternalId:  "ext-existing",
		Name:        "Existing Account",
		Description: "Already exists",
		Status:      "Enabled",
	}
	err := store.Insert(ctx, existingAccount)
	require.NoError(t, err)

	// Try to create another account with the same accountId for the same user
	duplicateAccountData := models.Account{
		AccountId:   accountId, // Same as existing account
		AccountType: "aws",
		RoleName:    "duplicate-role",
		ExternalId:  "ext-duplicate",
		Name:        "Duplicate Account",
		Description: "Should be rejected",
	}

	requestBody, err := json.Marshal(duplicateAccountData)
	require.NoError(t, err)

	// Create simple request event (authorization handled via ENV=TEST)
	request := events.APIGatewayV2HTTPRequest{
		Body: string(requestBody),
	}

	// Call the handler
	response, err := account.PostAccountsHandler(ctx, request)
	assert.NoError(t, err)

	// Verify 422 response for duplicate
	assert.Equal(t, 422, response.StatusCode)
	assert.Contains(t, response.Body, "error records exists")
}

func TestPostAccountsHandler_InvalidJSON(t *testing.T) {
	_, _ = setupPostAccountHandlerTest(t)
	ctx := context.Background()

	// Create request with invalid JSON (authorization handled via ENV=TEST)
	request := events.APIGatewayV2HTTPRequest{
		Body: "{ invalid json }",
	}

	// Call the handler
	response, err := account.PostAccountsHandler(ctx, request)
	assert.NoError(t, err)

	// Verify 400 response for malformed JSON (client error)
	assert.Equal(t, 400, response.StatusCode)
	assert.Contains(t, response.Body, "error unmarshaling body")
}