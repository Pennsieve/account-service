package account_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	account_handler "github.com/pennsieve/account-service/internal/handler/account"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/roleaccess/roleaccesstest"
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
	userId := test.UserNodeId("del-user-" + testId)

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
			Authorizer: test.CreateTestAuthorizer(test.UserNodeId("user-"+testId), ""),
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
			Authorizer: test.CreateTestAuthorizer(test.UserNodeId("user-"+testId), ""),
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
	ownerUserId := test.UserNodeId("owner-" + testId)
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
	userId := test.UserNodeId("del-conflict-user-" + testId)

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
		OrganizationId: test.OrgNodeId("conflict-" + testId),
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
	userId := test.UserNodeId("del-force-user-" + testId)

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
		OrganizationId: test.OrgNodeId("force-" + testId),
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
	userId := test.UserNodeId("del-ws-user-" + testId)

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

// An account whose role is gone (or no longer trusts Pennsieve) is
// disconnected even with nodes left: Pennsieve can't tear them down, so it
// removes its records and says what may remain in the AWS account.
func TestDeleteAccountHandler_RoleGoneDisconnects(t *testing.T) {
	accountStore, nodeStore, _, testId := setupDeleteAccountHandlerTest(t)
	ctx := context.Background()
	defer roleaccesstest.Answer("AccessDenied")()

	accountUuid := "del-rolegone-" + testId
	userId := test.UserNodeId("del-rolegone-user-" + testId)
	require.NoError(t, accountStore.Insert(ctx, store_dynamodb.Account{
		Uuid: accountUuid, AccountId: "123456789012", AccountType: "aws",
		RoleName: "test-role-" + testId, UserId: userId, Connection: models.ConnectionCloudFormation,
	}))
	require.NoError(t, nodeStore.Put(ctx, models.DynamoDBNode{
		Uuid: "node-rolegone-" + testId, AccountUuid: accountUuid,
		OrganizationId: test.OrgNodeId("rolegone-" + testId), Status: "Failed",
	}))

	response, err := account_handler.DeleteAccountHandler(ctx, events.APIGatewayV2HTTPRequest{
		PathParameters:        map[string]string{"id": accountUuid},
		QueryStringParameters: map[string]string{},
		RequestContext:        events.APIGatewayV2HTTPRequestContext{Authorizer: test.CreateTestAuthorizer(userId, "")},
	})
	require.NoError(t, err)
	require.Equal(t, 200, response.StatusCode, response.Body)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(response.Body), &body))
	assert.Equal(t, true, body["resourcesMayRemain"])
	assert.Equal(t, float64(1), body["computeNodesRemoved"])
	assert.Contains(t, body["runbookUrl"], "disconnect-without-pennsieve.md")
	assert.Equal(t, accountUuid, body["uuid"])

	node, err := nodeStore.GetById(ctx, "node-rolegone-"+testId)
	require.NoError(t, err)
	assert.Empty(t, node.Uuid, "the node's record is removed")
	acct, err := accountStore.GetById(ctx, accountUuid)
	require.NoError(t, err)
	assert.Empty(t, acct.Uuid, "the account is removed")
}

// A role check that fails for another reason (here throttling) keeps the
// refusal: nodes are only orphaned when the role is known to be unusable.
func TestDeleteAccountHandler_RoleCheckUnknownKeepsConflict(t *testing.T) {
	accountStore, nodeStore, _, testId := setupDeleteAccountHandlerTest(t)
	ctx := context.Background()
	defer roleaccesstest.Answer("Throttling")()

	accountUuid := "del-roleunknown-" + testId
	userId := test.UserNodeId("del-roleunknown-user-" + testId)
	require.NoError(t, accountStore.Insert(ctx, store_dynamodb.Account{
		Uuid: accountUuid, AccountId: "123456789012", AccountType: "aws",
		RoleName: "test-role-" + testId, UserId: userId, Connection: models.ConnectionCloudFormation,
	}))
	require.NoError(t, nodeStore.Put(ctx, models.DynamoDBNode{
		Uuid: "node-roleunknown-" + testId, AccountUuid: accountUuid,
		OrganizationId: test.OrgNodeId("roleunknown-" + testId), Status: "Enabled",
	}))

	response, err := account_handler.DeleteAccountHandler(ctx, events.APIGatewayV2HTTPRequest{
		PathParameters:        map[string]string{"id": accountUuid},
		QueryStringParameters: map[string]string{},
		RequestContext:        events.APIGatewayV2HTTPRequestContext{Authorizer: test.CreateTestAuthorizer(userId, "")},
	})
	require.NoError(t, err)
	assert.Equal(t, 409, response.StatusCode)
}

// An older connection's role may trust only the provisioner, so a refusal to
// this service proves nothing: the 409 stays, with a hint to upgrade first.
func TestDeleteAccountHandler_LegacyRoleKeepsConflict(t *testing.T) {
	accountStore, nodeStore, _, testId := setupDeleteAccountHandlerTest(t)
	ctx := context.Background()
	defer roleaccesstest.Answer("AccessDenied")()

	accountUuid := "del-legacy-" + testId
	userId := test.UserNodeId("del-legacy-user-" + testId)
	require.NoError(t, accountStore.Insert(ctx, store_dynamodb.Account{
		Uuid: accountUuid, AccountId: "123456789012", AccountType: "aws",
		RoleName: "ROLE-" + testId, UserId: userId,
	}))
	require.NoError(t, nodeStore.Put(ctx, models.DynamoDBNode{
		Uuid: "node-legacy-" + testId, AccountUuid: accountUuid,
		OrganizationId: test.OrgNodeId("legacy-" + testId), Status: "Enabled",
	}))

	response, err := account_handler.DeleteAccountHandler(ctx, events.APIGatewayV2HTTPRequest{
		PathParameters:        map[string]string{"id": accountUuid},
		QueryStringParameters: map[string]string{},
		RequestContext:        events.APIGatewayV2HTTPRequestContext{Authorizer: test.CreateTestAuthorizer(userId, "")},
	})
	require.NoError(t, err)
	require.Equal(t, 409, response.StatusCode)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(response.Body), &body))
	assert.Contains(t, body["error"], "upgrade the connection")
	node, err := nodeStore.GetById(ctx, "node-legacy-"+testId)
	require.NoError(t, err)
	assert.NotEmpty(t, node.Uuid, "the node's record stays")
}
