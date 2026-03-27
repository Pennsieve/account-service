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

func setupDeleteAccountHandlerTest(t *testing.T) (*store_dynamodb.AccountDatabaseStore, store_dynamodb.NodeStore, store_dynamodb.AccountWorkspaceStore, string) {
	testId := test.GenerateTestId()

	client := test.GetClient()
	accountStore := store_dynamodb.NewAccountDatabaseStore(client, TEST_ACCOUNTS_TABLE).(*store_dynamodb.AccountDatabaseStore)
	nodeStore := store_dynamodb.NewNodeDatabaseStore(client, TEST_NODES_TABLE)
	workspaceStore := store_dynamodb.NewAccountWorkspaceStore(client, TEST_WORKSPACE_TABLE)

	os.Setenv("ACCOUNTS_TABLE", TEST_ACCOUNTS_TABLE)
	os.Setenv("COMPUTE_NODES_TABLE", TEST_NODES_TABLE)
	os.Setenv("ACCOUNT_WORKSPACE_TABLE", TEST_WORKSPACE_TABLE)
	if os.Getenv("ENV") == "" {
		os.Setenv("ENV", "TEST")
	}
	if os.Getenv("DYNAMODB_URL") == "" {
		os.Setenv("DYNAMODB_URL", "http://localhost:8000")
	}

	return accountStore, nodeStore, workspaceStore, testId
}

func TestDeleteAccountHandler_Success(t *testing.T) {
	accountStore, _, _, testId := setupDeleteAccountHandlerTest(t)
	ctx := context.Background()

	accountUuid := "del-account-" + testId
	userId := "del-user-" + testId

	testAccount := store_dynamodb.Account{
		Uuid:        accountUuid,
		AccountId:   "acct-123-" + testId,
		AccountType: "aws",
		RoleName:    "test-role-" + testId,
		ExternalId:  "ext-123-" + testId,
		UserId:      userId,
	}
	err := accountStore.Insert(ctx, testAccount)
	require.NoError(t, err)

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": accountUuid},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, ""),
		},
	}

	response, err := account_handler.DeleteAccountHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 200, response.StatusCode)

	var responseAccount models.Account
	err = json.Unmarshal([]byte(response.Body), &responseAccount)
	assert.NoError(t, err)
	assert.Equal(t, accountUuid, responseAccount.Uuid)
	assert.Equal(t, "acct-123-"+testId, responseAccount.AccountId)

	// Verify account is deleted
	deleted, err := accountStore.GetById(ctx, accountUuid)
	assert.NoError(t, err)
	assert.Equal(t, store_dynamodb.Account{}, deleted)
}

func TestDeleteAccountHandler_MissingId(t *testing.T) {
	_, _, _, testId := setupDeleteAccountHandlerTest(t)
	ctx := context.Background()

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("user-"+testId, ""),
		},
	}

	response, err := account_handler.DeleteAccountHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 400, response.StatusCode)
}

func TestDeleteAccountHandler_NotFound(t *testing.T) {
	_, _, _, testId := setupDeleteAccountHandlerTest(t)
	ctx := context.Background()

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": "nonexistent-" + testId},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("user-"+testId, ""),
		},
	}

	response, err := account_handler.DeleteAccountHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 404, response.StatusCode)
}

func TestDeleteAccountHandler_Forbidden(t *testing.T) {
	accountStore, _, _, testId := setupDeleteAccountHandlerTest(t)
	ctx := context.Background()

	accountUuid := "del-forbidden-" + testId
	ownerUserId := "owner-" + testId
	otherUserId := "other-" + testId

	testAccount := store_dynamodb.Account{
		Uuid:        accountUuid,
		AccountId:   "acct-forbidden-" + testId,
		AccountType: "aws",
		RoleName:    "test-role-" + testId,
		ExternalId:  "ext-" + testId,
		UserId:      ownerUserId,
	}
	err := accountStore.Insert(ctx, testAccount)
	require.NoError(t, err)

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": accountUuid},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(otherUserId, ""),
		},
	}

	response, err := account_handler.DeleteAccountHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 403, response.StatusCode)
}

func TestDeleteAccountHandler_ConflictActiveNodes(t *testing.T) {
	accountStore, nodeStore, _, testId := setupDeleteAccountHandlerTest(t)
	ctx := context.Background()

	accountUuid := "del-conflict-" + testId
	userId := "del-conflict-user-" + testId

	testAccount := store_dynamodb.Account{
		Uuid:        accountUuid,
		AccountId:   "acct-conflict-" + testId,
		AccountType: "aws",
		RoleName:    "test-role-" + testId,
		ExternalId:  "ext-" + testId,
		UserId:      userId,
	}
	err := accountStore.Insert(ctx, testAccount)
	require.NoError(t, err)

	testNode := models.DynamoDBNode{
		Uuid:           "node-" + testId,
		AccountUuid:    accountUuid,
		OrganizationId: "N:organization:conflict-" + testId,
		Status:         "Enabled",
	}
	err = nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	request := events.APIGatewayV2HTTPRequest{
		PathParameters:        map[string]string{"id": accountUuid},
		QueryStringParameters: map[string]string{},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, ""),
		},
	}

	response, err := account_handler.DeleteAccountHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 409, response.StatusCode)

	var body map[string]interface{}
	err = json.Unmarshal([]byte(response.Body), &body)
	assert.NoError(t, err)
	assert.Equal(t, float64(1), body["nodeCount"])
}

func TestDeleteAccountHandler_ForceDeleteWithNodes(t *testing.T) {
	accountStore, nodeStore, _, testId := setupDeleteAccountHandlerTest(t)
	ctx := context.Background()

	accountUuid := "del-force-" + testId
	userId := "del-force-user-" + testId

	testAccount := store_dynamodb.Account{
		Uuid:        accountUuid,
		AccountId:   "acct-force-" + testId,
		AccountType: "aws",
		RoleName:    "test-role-" + testId,
		ExternalId:  "ext-" + testId,
		UserId:      userId,
	}
	err := accountStore.Insert(ctx, testAccount)
	require.NoError(t, err)

	testNode := models.DynamoDBNode{
		Uuid:           "node-force-" + testId,
		AccountUuid:    accountUuid,
		OrganizationId: "N:organization:force-" + testId,
		Status:         "Enabled",
	}
	err = nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	request := events.APIGatewayV2HTTPRequest{
		PathParameters:        map[string]string{"id": accountUuid},
		QueryStringParameters: map[string]string{"force": "true"},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, ""),
		},
	}

	response, err := account_handler.DeleteAccountHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 200, response.StatusCode)

	deleted, err := accountStore.GetById(ctx, accountUuid)
	assert.NoError(t, err)
	assert.Equal(t, store_dynamodb.Account{}, deleted)
}

func TestDeleteAccountHandler_DeletesWorkspaceEnablements(t *testing.T) {
	accountStore, _, workspaceStore, testId := setupDeleteAccountHandlerTest(t)
	ctx := context.Background()

	accountUuid := "del-ws-" + testId
	userId := "del-ws-user-" + testId

	testAccount := store_dynamodb.Account{
		Uuid:        accountUuid,
		AccountId:   "acct-ws-" + testId,
		AccountType: "aws",
		RoleName:    "test-role-" + testId,
		ExternalId:  "ext-" + testId,
		UserId:      userId,
	}
	err := accountStore.Insert(ctx, testAccount)
	require.NoError(t, err)

	enablement := store_dynamodb.AccountWorkspace{
		AccountUuid: accountUuid,
		WorkspaceId: "ws-" + testId,
	}
	err = workspaceStore.Insert(ctx, enablement)
	require.NoError(t, err)

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": accountUuid},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, ""),
		},
	}

	response, err := account_handler.DeleteAccountHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 200, response.StatusCode)

	enablements, err := workspaceStore.GetByAccount(ctx, accountUuid)
	assert.NoError(t, err)
	assert.Empty(t, enablements)
}
