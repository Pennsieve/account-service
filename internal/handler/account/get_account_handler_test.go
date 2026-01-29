package account_test

import (
	"context"
	"encoding/json"
	"log"
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

// Shared table names for all account handler tests
const (
	TEST_ACCOUNTS_TABLE            = "test-accounts-table"
	TEST_ACCOUNTS_WITH_INDEX_TABLE = "test-accounts-with-index-table"
	TEST_NODES_TABLE               = "test-nodes-table"
	TEST_ACCESS_TABLE              = "test-access-table"
	TEST_WORKSPACE_TABLE           = "test-workspace-table"
)

// TestMain sets up tables once for the entire account handler package
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

func setupAccountHandlerTest(t *testing.T) (*store_dynamodb.AccountDatabaseStore, string) {
	// Generate unique test ID for isolation
	testId := test.GenerateTestId()
	
	// Use shared test client and accounts table
	client := test.GetClient()
	store := store_dynamodb.NewAccountDatabaseStore(client, TEST_ACCOUNTS_TABLE).(*store_dynamodb.AccountDatabaseStore)
	
	// Set environment variables for handler to use test client
	os.Setenv("ACCOUNTS_TABLE", TEST_ACCOUNTS_TABLE)
	// Don't override ENV if already set (Docker sets it to DOCKER)
	if os.Getenv("ENV") == "" {
		os.Setenv("ENV", "TEST")  // This triggers test-aware config in LoadAWSConfig
	}
	// Don't override DYNAMODB_URL if already set (Docker sets it to http://dynamodb:8000)
	if os.Getenv("DYNAMODB_URL") == "" {
		os.Setenv("DYNAMODB_URL", "http://localhost:8000")  // Local DynamoDB endpoint for local testing
	}
	
	// Register cleanup for this test's specific data
	t.Cleanup(func() {
		// Clean up only this test's data using unique IDs
		// Note: In a real cleanup, you'd want to remove specific records
		// For now, rely on unique IDs to prevent conflicts
	})

	return store, testId
}

func TestGetAccountHandler_Success(t *testing.T) {
	store, testId := setupAccountHandlerTest(t)
	ctx := context.Background()

	// Create test data with unique IDs
	accountUuid := "account-uuid-" + testId
	accountId := "account-123-" + testId
	userId := "user-456-" + testId

	testAccount := store_dynamodb.Account{
		Uuid:        accountUuid,
		AccountId:   accountId,
		AccountType: "aws",
		RoleName:    "test-role-" + testId,
		ExternalId:  "ext-123-" + testId,
		UserId:      userId,
		Name:        "Test Account " + testId,
		Description: "Test account for handler testing",
		Status:      "active",
	}

	// Insert test account
	err := store.Insert(ctx, testAccount)
	require.NoError(t, err)

	// Create request event
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": accountUuid,
		},
	}

	// Call the handler
	response, err := account.GetAccountHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, 200, response.StatusCode)

	// Parse response body
	var responseAccount models.Account
	err = json.Unmarshal([]byte(response.Body), &responseAccount)
	assert.NoError(t, err)

	// Verify account data
	assert.Equal(t, accountUuid, responseAccount.Uuid)
	assert.Equal(t, accountId, responseAccount.AccountId)
	assert.Equal(t, "aws", responseAccount.AccountType)
	assert.Equal(t, "test-role-"+testId, responseAccount.RoleName)
	assert.Equal(t, "ext-123-"+testId, responseAccount.ExternalId)
	assert.Equal(t, userId, responseAccount.UserId)
	assert.Equal(t, "Test Account "+testId, responseAccount.Name)
	assert.Equal(t, "Test account for handler testing", responseAccount.Description)
	assert.Equal(t, "active", responseAccount.Status)

	// No explicit cleanup needed - using unique IDs prevents conflicts
}

func TestGetAccountHandler_NotFound(t *testing.T) {
	_, testId := setupAccountHandlerTest(t)
	ctx := context.Background()

	// Use non-existent account UUID with unique test ID
	nonExistentUuid := "non-existent-" + testId

	// Create request event
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": nonExistentUuid,
		},
	}

	// Call the handler
	response, err := account.GetAccountHandler(ctx, request)
	assert.NoError(t, err)

	// Verify 404 response
	assert.Equal(t, 404, response.StatusCode)
	assert.Contains(t, response.Body, "error no records found")
}

func TestGetAccountHandler_InvalidUuid(t *testing.T) {
	_, _ = setupAccountHandlerTest(t)
	ctx := context.Background()

	// Use non-existent UUID (this should result in 404, not validation error)
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": "invalid-uuid-format",
		},
	}

	// Call the handler
	response, err := account.GetAccountHandler(ctx, request)
	assert.NoError(t, err)

	// Verify 404 response for non-existent UUID
	assert.Equal(t, 404, response.StatusCode)
}