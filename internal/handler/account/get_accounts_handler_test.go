package account_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/account-service/internal/handler/account"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupGetAccountsHandlerTest(t *testing.T) (*store_dynamodb.AccountDatabaseStore, *store_dynamodb.AccountWorkspaceStoreImpl, string) {
	// Generate unique test ID for isolation
	testId := test.GenerateTestId()
	
	// Use shared test client and accounts table with index
	client := test.GetClient()
	accountStore := store_dynamodb.NewAccountDatabaseStore(client, TEST_ACCOUNTS_WITH_INDEX_TABLE).(*store_dynamodb.AccountDatabaseStore)
	workspaceStore := store_dynamodb.NewAccountWorkspaceStore(client, TEST_WORKSPACE_TABLE).(*store_dynamodb.AccountWorkspaceStoreImpl)
	
	// Set environment variables for handler to use test client
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

	return accountStore, workspaceStore, testId
}

func TestGetAccountsHandler_Success_NoAccounts(t *testing.T) {
	_, _, testId := setupGetAccountsHandlerTest(t)
	ctx := context.Background()

	// Create simple request with test authorizer (no query parameters)
	request := events.APIGatewayV2HTTPRequest{
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testId, ""),
		},
	}

	// Call the handler
	response, err := account.GetAccountsHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, 200, response.StatusCode)
	
	// Parse response body - should be empty array
	var accounts []models.Account
	err = json.Unmarshal([]byte(response.Body), &accounts)
	assert.NoError(t, err)
	assert.Empty(t, accounts)
}

func TestGetAccountsHandler_Success_WithAccounts(t *testing.T) {
	accountStore, _, testId := setupGetAccountsHandlerTest(t)
	ctx := context.Background()

	// Create test accounts for this user
	userId := testId
	testAccounts := []store_dynamodb.Account{
		{
			Uuid:        "account-1-" + testId,
			UserId:      userId,
			AccountId:   "123456789012",
			AccountType: "aws",
			RoleName:    "test-role-1",
			ExternalId:  "ext-1-" + testId,
			Name:        "Test Account 1",
			Description: "First test account",
			Status:      "Enabled",
		},
		{
			Uuid:        "account-2-" + testId,
			UserId:      userId,
			AccountId:   "123456789013",
			AccountType: "aws",
			RoleName:    "test-role-2",
			ExternalId:  "ext-2-" + testId,
			Name:        "Test Account 2",
			Description: "Second test account",
			Status:      "Paused",
		},
	}

	// Insert test accounts
	for _, acc := range testAccounts {
		err := accountStore.Insert(ctx, acc)
		require.NoError(t, err)
	}

	// Create simple request with test authorizer (no query parameters)
	request := events.APIGatewayV2HTTPRequest{
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testId, ""),
		},
	}

	// Call the handler
	response, err := account.GetAccountsHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, 200, response.StatusCode)
	
	// Parse response body
	var accounts []models.Account
	err = json.Unmarshal([]byte(response.Body), &accounts)
	assert.NoError(t, err)
	require.Len(t, accounts, 2)

	// Verify account details (order might vary)
	accountMap := make(map[string]models.Account)
	for _, acc := range accounts {
		accountMap[acc.Uuid] = acc
	}

	assert.Contains(t, accountMap, "account-1-"+testId)
	assert.Contains(t, accountMap, "account-2-"+testId)
	
	acc1 := accountMap["account-1-"+testId]
	assert.Equal(t, "123456789012", acc1.AccountId)
	assert.Equal(t, "aws", acc1.AccountType)
	assert.Equal(t, "test-role-1", acc1.RoleName)
	assert.Equal(t, "Test Account 1", acc1.Name)
	assert.Equal(t, "Enabled", acc1.Status)

	acc2 := accountMap["account-2-"+testId]
	assert.Equal(t, "123456789013", acc2.AccountId)
	assert.Equal(t, "Paused", acc2.Status)
}

func TestGetAccountsHandler_Success_WorkspaceFilter(t *testing.T) {
	accountStore, workspaceStore, testId := setupGetAccountsHandlerTest(t)
	ctx := context.Background()

	userId := testId
	workspaceId := "workspace-1-" + testId
	
	// Create test accounts
	testAccounts := []store_dynamodb.Account{
		{
			Uuid:        "account-enabled-" + testId,
			UserId:      userId,
			AccountId:   "123456789012",
			AccountType: "aws",
			RoleName:    "enabled-role",
			ExternalId:  "ext-enabled-" + testId,
			Name:        "Enabled Account",
			Description: "Account enabled for workspace",
			Status:      "Enabled",
		},
		{
			Uuid:        "account-not-enabled-" + testId,
			UserId:      userId,
			AccountId:   "123456789013",
			AccountType: "aws",
			RoleName:    "not-enabled-role",
			ExternalId:  "ext-not-enabled-" + testId,
			Name:        "Not Enabled Account",
			Description: "Account not enabled for workspace",
			Status:      "Enabled",
		},
	}

	// Insert test accounts
	for _, acc := range testAccounts {
		err := accountStore.Insert(ctx, acc)
		require.NoError(t, err)
	}

	// Create workspace enablement for first account only
	enablement := store_dynamodb.AccountWorkspace{
		AccountUuid: "account-enabled-" + testId,
		WorkspaceId: workspaceId,
		IsPublic:    true,
		EnabledBy:   userId,
		EnabledAt:   time.Now().Unix(),
	}
	err := workspaceStore.Insert(ctx, enablement)
	require.NoError(t, err)

	// Create request with workspace filter and test authorizer
	request := events.APIGatewayV2HTTPRequest{
		QueryStringParameters: map[string]string{
			"workspace": workspaceId,
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testId, ""),
		},
	}

	// Call the handler
	response, err := account.GetAccountsHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, 200, response.StatusCode)
	
	// Parse response body
	var accounts []models.Account
	err = json.Unmarshal([]byte(response.Body), &accounts)
	assert.NoError(t, err)
	
	// Should only return the enabled account
	require.Len(t, accounts, 1)
	assert.Equal(t, "account-enabled-"+testId, accounts[0].Uuid)
	assert.Equal(t, "Enabled Account", accounts[0].Name)
}

func TestGetAccountsHandler_Success_IncludeWorkspaces(t *testing.T) {
	accountStore, workspaceStore, testId := setupGetAccountsHandlerTest(t)
	ctx := context.Background()

	userId := testId
	workspaceId1 := "workspace-1-" + testId
	workspaceId2 := "workspace-2-" + testId
	
	// Create test account
	testAccount := store_dynamodb.Account{
		Uuid:        "account-with-workspaces-" + testId,
		UserId:      userId,
		AccountId:   "123456789012",
		AccountType: "aws",
		RoleName:    "test-role",
		ExternalId:  "ext-" + testId,
		Name:        "Account With Workspaces",
		Description: "Account with multiple workspace enablements",
		Status:      "Enabled",
	}

	err := accountStore.Insert(ctx, testAccount)
	require.NoError(t, err)

	// Create workspace enablements
	enablements := []store_dynamodb.AccountWorkspace{
		{
			AccountUuid: testAccount.Uuid,
			WorkspaceId: workspaceId1,
			IsPublic:    true,
			EnabledBy:   userId,
			EnabledAt:   time.Now().Unix(),
		},
		{
			AccountUuid: testAccount.Uuid,
			WorkspaceId: workspaceId2,
			IsPublic:    false,
			EnabledBy:   userId,
			EnabledAt:   time.Now().Unix() + 100,
		},
	}

	for _, enablement := range enablements {
		err := workspaceStore.Insert(ctx, enablement)
		require.NoError(t, err)
	}

	// Create request with includeWorkspaces=true and test authorizer
	request := events.APIGatewayV2HTTPRequest{
		QueryStringParameters: map[string]string{
			"includeWorkspaces": "true",
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testId, ""),
		},
	}

	// Call the handler
	response, err := account.GetAccountsHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, 200, response.StatusCode)
	
	// Parse response body - should be AccountWithWorkspaces format
	var accountsWithWorkspaces []models.AccountWithWorkspaces
	err = json.Unmarshal([]byte(response.Body), &accountsWithWorkspaces)
	assert.NoError(t, err)
	
	require.Len(t, accountsWithWorkspaces, 1)
	
	accountWithWorkspaces := accountsWithWorkspaces[0]
	assert.Equal(t, testAccount.Uuid, accountWithWorkspaces.Account.Uuid)
	assert.Equal(t, testAccount.Name, accountWithWorkspaces.Account.Name)
	
	// Should have 2 workspace enablements
	require.Len(t, accountWithWorkspaces.EnabledWorkspaces, 2)
	
	// Verify workspace enablements (order might vary)
	enablementMap := make(map[string]models.AccountWorkspaceEnablement)
	for _, e := range accountWithWorkspaces.EnabledWorkspaces {
		enablementMap[e.OrganizationId] = e
	}
	
	assert.Contains(t, enablementMap, workspaceId1)
	assert.Contains(t, enablementMap, workspaceId2)
	
	assert.True(t, enablementMap[workspaceId1].IsPublic)
	assert.False(t, enablementMap[workspaceId2].IsPublic)
}

func TestGetAccountsHandler_Success_CombinedFilters(t *testing.T) {
	accountStore, workspaceStore, testId := setupGetAccountsHandlerTest(t)
	ctx := context.Background()

	userId := testId
	workspaceId := "workspace-filter-" + testId
	
	// Create test account enabled for specific workspace
	testAccount := store_dynamodb.Account{
		Uuid:        "account-combined-" + testId,
		UserId:      userId,
		AccountId:   "123456789012",
		AccountType: "aws",
		RoleName:    "combined-role",
		ExternalId:  "ext-combined-" + testId,
		Name:        "Combined Filter Account",
		Description: "Account for combined filter test",
		Status:      "Enabled",
	}

	err := accountStore.Insert(ctx, testAccount)
	require.NoError(t, err)

	// Create workspace enablement
	enablement := store_dynamodb.AccountWorkspace{
		AccountUuid: testAccount.Uuid,
		WorkspaceId: workspaceId,
		IsPublic:    true,
		EnabledBy:   userId,
		EnabledAt:   time.Now().Unix(),
	}
	err = workspaceStore.Insert(ctx, enablement)
	require.NoError(t, err)

	// Create request with both workspace filter and includeWorkspaces and test authorizer
	request := events.APIGatewayV2HTTPRequest{
		QueryStringParameters: map[string]string{
			"workspace":         workspaceId,
			"includeWorkspaces": "true",
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testId, ""),
		},
	}

	// Call the handler
	response, err := account.GetAccountsHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, 200, response.StatusCode)
	
	// Parse response body
	var accountsWithWorkspaces []models.AccountWithWorkspaces
	err = json.Unmarshal([]byte(response.Body), &accountsWithWorkspaces)
	assert.NoError(t, err)
	
	require.Len(t, accountsWithWorkspaces, 1)
	
	accountWithWorkspaces := accountsWithWorkspaces[0]
	assert.Equal(t, testAccount.Uuid, accountWithWorkspaces.Account.Uuid)
	assert.Equal(t, testAccount.Name, accountWithWorkspaces.Account.Name)
	
	// Should include workspace details
	require.Len(t, accountWithWorkspaces.EnabledWorkspaces, 1)
	assert.Equal(t, workspaceId, accountWithWorkspaces.EnabledWorkspaces[0].OrganizationId)
	assert.True(t, accountWithWorkspaces.EnabledWorkspaces[0].IsPublic)
}

func TestGetAccountsHandler_Success_EmptyWorkspaceFilter(t *testing.T) {
	accountStore, _, testId := setupGetAccountsHandlerTest(t)
	ctx := context.Background()

	userId := testId
	
	// Create test account (no workspace enablements)
	testAccount := store_dynamodb.Account{
		Uuid:        "account-no-workspace-" + testId,
		UserId:      userId,
		AccountId:   "123456789012",
		AccountType: "aws",
		RoleName:    "test-role",
		ExternalId:  "ext-" + testId,
		Name:        "Account No Workspaces",
		Description: "Account without workspace enablements",
		Status:      "Enabled",
	}

	err := accountStore.Insert(ctx, testAccount)
	require.NoError(t, err)

	// Create request with workspace filter for non-existent workspace and test authorizer
	request := events.APIGatewayV2HTTPRequest{
		QueryStringParameters: map[string]string{
			"workspace": "non-existent-workspace",
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testId, ""),
		},
	}

	// Call the handler
	response, err := account.GetAccountsHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, 200, response.StatusCode)
	
	// Parse response body - should be empty since no accounts enabled for this workspace
	var accounts []models.Account
	err = json.Unmarshal([]byte(response.Body), &accounts)
	assert.NoError(t, err)
	assert.Empty(t, accounts)
}

func TestGetAccountsHandler_Success_IncludeWorkspaces_NoEnablements(t *testing.T) {
	accountStore, _, testId := setupGetAccountsHandlerTest(t)
	ctx := context.Background()

	userId := testId
	
	// Create test account without any workspace enablements
	testAccount := store_dynamodb.Account{
		Uuid:        "account-no-enablements-" + testId,
		UserId:      userId,
		AccountId:   "123456789012",
		AccountType: "aws",
		RoleName:    "test-role",
		ExternalId:  "ext-" + testId,
		Name:        "Account No Enablements",
		Description: "Account without workspace enablements",
		Status:      "Enabled",
	}

	err := accountStore.Insert(ctx, testAccount)
	require.NoError(t, err)

	// Create request with includeWorkspaces=true and test authorizer
	request := events.APIGatewayV2HTTPRequest{
		QueryStringParameters: map[string]string{
			"includeWorkspaces": "true",
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testId, ""),
		},
	}

	// Call the handler
	response, err := account.GetAccountsHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, 200, response.StatusCode)
	
	// Parse response body
	var accountsWithWorkspaces []models.AccountWithWorkspaces
	err = json.Unmarshal([]byte(response.Body), &accountsWithWorkspaces)
	assert.NoError(t, err)
	
	require.Len(t, accountsWithWorkspaces, 1)
	
	accountWithWorkspaces := accountsWithWorkspaces[0]
	assert.Equal(t, testAccount.Uuid, accountWithWorkspaces.Account.Uuid)
	
	// Should have empty workspace enablements
	assert.Empty(t, accountWithWorkspaces.EnabledWorkspaces)
}