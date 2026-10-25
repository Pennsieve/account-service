package account_test

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
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
	
	// Set environment variables for handler to use test client
	os.Setenv("ACCOUNTS_TABLE", TEST_ACCOUNTS_WITH_INDEX_TABLE)
	os.Setenv("ROLE_CODES_TABLE", test.TEST_ROLE_CODES_TABLE)
	// Don't override ENV if already set (Docker sets it to DOCKER)
	if os.Getenv("ENV") == "" {
		os.Setenv("ENV", "TEST")  // This triggers test-aware config in LoadAWSConfig
	}
	// Don't override DYNAMODB_URL if already set (Docker sets it to http://dynamodb:8000)
	if os.Getenv("DYNAMODB_URL") == "" {
		os.Setenv("DYNAMODB_URL", "http://localhost:8000")  // Local DynamoDB endpoint for local testing
	}
	
	// STS answers like a role whose trust policy requires the external ID
	// the request carries (or any, for a legacy request without one).
	useRoleAssumer(t, &stsFake{})

	return store, testId
}

// stsFake answers AssumeRole like the role registration is checking: usable
// unless unusable, and requiring an external ID (the one sent with it) unless
// unconditional.
type stsFake struct {
	unusable      bool
	unconditional bool
	seenId        string
}

func (f *stsFake) AssumeRole(_ context.Context, in *sts.AssumeRoleInput, _ ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	if f.unusable {
		return nil, errors.New("AccessDenied")
	}
	if in.ExternalId != nil {
		f.seenId = *in.ExternalId
		return &sts.AssumeRoleOutput{}, nil
	}
	if f.seenId != "" && !f.unconditional {
		return nil, errors.New("AccessDenied")
	}
	return &sts.AssumeRoleOutput{}, nil
}

func useRoleAssumer(t *testing.T, f account.RoleAssumer) {
	prev := account.NewRoleAssumer
	account.NewRoleAssumer = func(aws.Config) account.RoleAssumer { return f }
	t.Cleanup(func() { account.NewRoleAssumer = prev })
}

// externalIdFor is a valid, per-test external ID.
func externalIdFor(seed string) string {
	return fmt.Sprintf("pn-%x", md5.Sum([]byte(seed)))
}

// issued records externalId as issued to userId for roleName, as GET
// /role-policy does, and returns it.
func issued(t *testing.T, externalId, userId, roleName string) string {
	t.Helper()
	now := time.Now()
	require.NoError(t, roleCodes().Issue(context.Background(), store_dynamodb.RoleCode{
		ExternalId: externalId, UserId: userId, RoleName: roleName,
		IssuedAt: now.Unix(), ExpiresAt: now.Add(24 * time.Hour).Unix(),
	}))
	return externalId
}

func roleCodes() store_dynamodb.RoleCodeStore {
	return store_dynamodb.NewRoleCodeStore(test.GetClient(), test.TEST_ROLE_CODES_TABLE)
}

func postAccount(t *testing.T, userId string, a models.Account) events.APIGatewayV2HTTPResponse {
	t.Helper()
	body, err := json.Marshal(a)
	require.NoError(t, err)
	resp, err := account.PostAccountsHandler(context.Background(), events.APIGatewayV2HTTPRequest{
		Body:           string(body),
		RequestContext: events.APIGatewayV2HTTPRequestContext{Authorizer: test.CreateTestAuthorizer(userId, "")},
	})
	require.NoError(t, err)
	return resp
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
		ExternalId:  issued(t, externalIdFor(testId), testId, "test-role-"+testId),
		Name:        "Test Account " + testId,
		Description: "Test account for handler testing",
	}

	requestBody, err := json.Marshal(accountData)
	require.NoError(t, err)

	// Create request with test authorizer
	request := events.APIGatewayV2HTTPRequest{
		Body: string(requestBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, ""),
		},
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
		ExternalId:  externalIdFor("existing" + testId),
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
		ExternalId:  externalIdFor("duplicate" + testId),
		Name:        "Duplicate Account",
		Description: "Should be rejected",
	}

	requestBody, err := json.Marshal(duplicateAccountData)
	require.NoError(t, err)

	// Create request with test authorizer
	request := events.APIGatewayV2HTTPRequest{
		Body: string(requestBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, ""),
		},
	}

	// Call the handler
	response, err := account.PostAccountsHandler(ctx, request)
	assert.NoError(t, err)

	// Verify 422 response for duplicate
	assert.Equal(t, 422, response.StatusCode)
	assert.Contains(t, response.Body, "error records exists")
}

func TestPostAccountsHandler_InvalidJSON(t *testing.T) {
	_, testId := setupPostAccountHandlerTest(t)
	ctx := context.Background()

	// Create request with invalid JSON but valid authorizer
	request := events.APIGatewayV2HTTPRequest{
		Body: "{ invalid json }",
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testId, ""),
		},
	}

	// Call the handler
	response, err := account.PostAccountsHandler(ctx, request)
	assert.NoError(t, err)

	// Verify 400 response for malformed JSON (client error)
	assert.Equal(t, 400, response.StatusCode)
	assert.Contains(t, response.Body, "error unmarshaling body")
}
func TestPostAccountsHandler_InvalidExternalId(t *testing.T) {
	_, testId := setupPostAccountHandlerTest(t)
	resp := postAccount(t, testId, models.Account{AccountId: "acct-" + testId, AccountType: "aws", RoleName: "r-" + testId, ExternalId: "ext-123"})
	assert.Equal(t, 400, resp.StatusCode)
	assert.Contains(t, resp.Body, "invalid external id")
}

func TestPostAccountsHandler_RoleUnusable(t *testing.T) {
	store, testId := setupPostAccountHandlerTest(t)
	useRoleAssumer(t, &stsFake{unusable: true})
	account.ShortenVerifyRetries(t)
	resp := postAccount(t, testId, models.Account{AccountId: "acct-" + testId, AccountType: "aws", RoleName: "r-" + testId, ExternalId: issued(t, externalIdFor(testId), testId, "r-"+testId)})
	assert.Equal(t, 400, resp.StatusCode)
	assert.Contains(t, resp.Body, "couldn't use the role")
	stored, err := store.GetByAccountId(context.Background(), "acct-"+testId)
	require.NoError(t, err)
	assert.Empty(t, stored, "an unusable role must not be registered")
}

func TestPostAccountsHandler_TrustWithoutExternalIdCondition(t *testing.T) {
	_, testId := setupPostAccountHandlerTest(t)
	useRoleAssumer(t, &stsFake{unconditional: true})
	resp := postAccount(t, testId, models.Account{AccountId: "acct-" + testId, AccountType: "aws", RoleName: "r-" + testId, ExternalId: issued(t, externalIdFor(testId), testId, "r-"+testId)})
	assert.Equal(t, 400, resp.StatusCode)
	assert.Contains(t, resp.Body, "doesn't require its external ID")
}

func TestPostAccountsHandler_RoleRegisteredByAnotherUser(t *testing.T) {
	store, testId := setupPostAccountHandlerTest(t)
	require.NoError(t, store.Insert(context.Background(), store_dynamodb.Account{
		Uuid: "other-" + testId, UserId: "other-user-" + testId, AccountId: "acct-" + testId,
		AccountType: "aws", RoleName: "r-" + testId, ExternalId: externalIdFor("other" + testId), Status: "Enabled",
	}))
	resp := postAccount(t, testId, models.Account{AccountId: "acct-" + testId, AccountType: "aws", RoleName: "r-" + testId, ExternalId: issued(t, externalIdFor(testId), testId, "r-"+testId)})
	assert.Equal(t, 409, resp.StatusCode)
	assert.Contains(t, resp.Body, `"message":"this role is already registered with Pennsieve"`)
}

func TestPostAccountsHandler_LegacyWithoutExternalId(t *testing.T) {
	store, testId := setupPostAccountHandlerTest(t)
	resp := postAccount(t, testId, models.Account{AccountId: "acct-" + testId, AccountType: "aws", RoleName: "r-" + testId})
	require.Equal(t, 201, resp.StatusCode, resp.Body)
	var created models.AccountResponse
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &created))
	stored, err := store.GetById(context.Background(), created.Uuid)
	require.NoError(t, err)
	assert.Empty(t, stored.ExternalId)
}

func TestPostAccountsHandler_ExternalIdIssuedToAnotherUser(t *testing.T) {
	store, testId := setupPostAccountHandlerTest(t)
	code := issued(t, externalIdFor(testId), "someone-else-"+testId, "r-"+testId)
	resp := postAccount(t, testId, models.Account{AccountId: "acct-" + testId, AccountType: "aws", RoleName: "r-" + testId, ExternalId: code})
	assert.Equal(t, 403, resp.StatusCode)
	assert.Contains(t, resp.Body, "wasn't issued to you")
	stored, err := store.GetByAccountId(context.Background(), "acct-"+testId)
	require.NoError(t, err)
	assert.Empty(t, stored)
}

func TestPostAccountsHandler_ExternalIdNeverIssued(t *testing.T) {
	_, testId := setupPostAccountHandlerTest(t)
	resp := postAccount(t, testId, models.Account{AccountId: "acct-" + testId, AccountType: "aws", RoleName: "r-" + testId, ExternalId: externalIdFor(testId)})
	assert.Equal(t, 403, resp.StatusCode)
}

func TestPostAccountsHandler_ExternalIdUsedOnce(t *testing.T) {
	_, testId := setupPostAccountHandlerTest(t)
	code := issued(t, externalIdFor(testId), testId, "r-"+testId)
	first := postAccount(t, testId, models.Account{AccountId: "acct-" + testId, AccountType: "aws", RoleName: "r-" + testId, ExternalId: code})
	require.Equal(t, 201, first.StatusCode, first.Body)
	used, found, err := roleCodes().Get(context.Background(), code)
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, used.Used)

	// The same code again, for another account: refused, the code is used.
	again := postAccount(t, testId, models.Account{AccountId: "acct2-" + testId, AccountType: "aws", RoleName: "r-" + testId, ExternalId: code})
	assert.Equal(t, 403, again.StatusCode)
}

// A registration whose role check fails (IAM still propagating) keeps its
// code: the retry registers.
func TestPostAccountsHandler_FailedVerificationKeepsTheCode(t *testing.T) {
	_, testId := setupPostAccountHandlerTest(t)
	code := issued(t, externalIdFor(testId), testId, "r-"+testId)
	useRoleAssumer(t, &stsFake{unusable: true})
	account.ShortenVerifyRetries(t)
	failed := postAccount(t, testId, models.Account{AccountId: "acct-" + testId, AccountType: "aws", RoleName: "r-" + testId, ExternalId: code})
	require.Equal(t, 400, failed.StatusCode)

	useRoleAssumer(t, &stsFake{})
	retry := postAccount(t, testId, models.Account{AccountId: "acct-" + testId, AccountType: "aws", RoleName: "r-" + testId, ExternalId: code})
	assert.Equal(t, 201, retry.StatusCode, retry.Body)
}

