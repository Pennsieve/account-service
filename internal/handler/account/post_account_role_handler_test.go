package account_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/aws-sdk-go-v2/service/sts/types"
	"github.com/pennsieve/account-service/internal/handler/account"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// legacyAccount is an account registered before the template: no external ID,
// no connection.
func legacyAccount(t *testing.T, store *store_dynamodb.AccountDatabaseStore, testId string) store_dynamodb.Account {
	t.Helper()
	a := store_dynamodb.Account{
		Uuid: "acct-uuid-" + testId, UserId: testId, AccountId: "123456789012",
		AccountType: "aws", RoleName: "ROLE-old-" + testId, Status: "Enabled",
	}
	require.NoError(t, store.Insert(context.Background(), a))
	return a
}

func postRole(t *testing.T, userId, accountUuid string, body account.AccountRoleRequest) events.APIGatewayV2HTTPResponse {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	resp, err := account.PostAccountRoleHandler(context.Background(), events.APIGatewayV2HTTPRequest{
		Body:           string(b),
		PathParameters: map[string]string{"uuid": accountUuid},
		RequestContext: events.APIGatewayV2HTTPRequestContext{Authorizer: test.CreateTestAuthorizer(userId, "")},
	})
	require.NoError(t, err)
	return resp
}

func TestPostAccountRole_UpgradesALegacyAccount(t *testing.T) {
	store, testId := setupPostAccountHandlerTest(t)
	t.Setenv("CFN_TEMPLATE_VERSION", "2")
	old := legacyAccount(t, store, testId)
	newRole := "Pennsieve-Compute-dev-" + testId
	code := issued(t, externalIdFor(testId), testId, newRole)

	resp := postRole(t, testId, old.Uuid, account.AccountRoleRequest{RoleName: newRole, ExternalId: code})
	require.Equal(t, 200, resp.StatusCode, resp.Body)
	var out account.AccountRoleResponse
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &out))
	assert.Equal(t, old.RoleName, out.OldRoleName)
	assert.Equal(t, newRole, out.RoleName)
	assert.Equal(t, "cloudformation", out.Connection)
	assert.Equal(t, "2", out.TemplateVersion, "the published version when the request names none")

	stored, err := store.GetById(context.Background(), old.Uuid)
	require.NoError(t, err)
	assert.Equal(t, newRole, stored.RoleName)
	assert.Equal(t, code, stored.ExternalId)
	assert.Equal(t, models.ConnectionCloudFormation, stored.Connection)
	assert.Equal(t, "2", stored.TemplateVersion)
	assert.Equal(t, old.AccountId, stored.AccountId)
	assert.Equal(t, "Enabled", stored.Status, "everything else on the record stays")

	used, _, err := roleCodes().Get(context.Background(), code)
	require.NoError(t, err)
	assert.True(t, used.Used)
}

func TestPostAccountRole_Refusals(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(t *testing.T, store *store_dynamodb.AccountDatabaseStore, testId string) (userId, accountUuid string, body account.AccountRoleRequest)
		status int
		body   string
	}{
		{"not the owner", func(t *testing.T, store *store_dynamodb.AccountDatabaseStore, testId string) (string, string, account.AccountRoleRequest) {
			a := legacyAccount(t, store, testId)
			code := issued(t, externalIdFor(testId), "admin-"+testId, "new-"+testId)
			return "admin-" + testId, a.Uuid, account.AccountRoleRequest{RoleName: "new-" + testId, ExternalId: code}
		}, 403, ""},
		{"code issued to someone else", func(t *testing.T, store *store_dynamodb.AccountDatabaseStore, testId string) (string, string, account.AccountRoleRequest) {
			a := legacyAccount(t, store, testId)
			code := issued(t, externalIdFor(testId), "mallory-"+testId, "new-"+testId)
			return testId, a.Uuid, account.AccountRoleRequest{RoleName: "new-" + testId, ExternalId: code}
		}, 403, "wasn't issued to you"},
		{"code for another role", func(t *testing.T, store *store_dynamodb.AccountDatabaseStore, testId string) (string, string, account.AccountRoleRequest) {
			a := legacyAccount(t, store, testId)
			code := issued(t, externalIdFor(testId), testId, "other-"+testId)
			return testId, a.Uuid, account.AccountRoleRequest{RoleName: "new-" + testId, ExternalId: code}
		}, 403, "wasn't issued to you"},
		{"the role it already uses", func(t *testing.T, store *store_dynamodb.AccountDatabaseStore, testId string) (string, string, account.AccountRoleRequest) {
			a := legacyAccount(t, store, testId)
			code := issued(t, externalIdFor(testId), testId, a.RoleName)
			return testId, a.Uuid, account.AccountRoleRequest{RoleName: a.RoleName, ExternalId: code}
		}, 400, "already uses that role"},
		{"another account's role", func(t *testing.T, store *store_dynamodb.AccountDatabaseStore, testId string) (string, string, account.AccountRoleRequest) {
			a := legacyAccount(t, store, testId)
			require.NoError(t, store.Insert(context.Background(), store_dynamodb.Account{
				Uuid: "other-uuid-" + testId, UserId: "someone-" + testId, AccountId: a.AccountId, AccountType: "aws", RoleName: "new-" + testId,
			}))
			code := issued(t, externalIdFor(testId), testId, "new-"+testId)
			return testId, a.Uuid, account.AccountRoleRequest{RoleName: "new-" + testId, ExternalId: code}
		}, 409, "already registered"},
		{"no such account", func(t *testing.T, store *store_dynamodb.AccountDatabaseStore, testId string) (string, string, account.AccountRoleRequest) {
			code := issued(t, externalIdFor(testId), testId, "new-"+testId)
			return testId, "missing-" + testId, account.AccountRoleRequest{RoleName: "new-" + testId, ExternalId: code}
		}, 404, ""},
		{"no external id", func(t *testing.T, store *store_dynamodb.AccountDatabaseStore, testId string) (string, string, account.AccountRoleRequest) {
			a := legacyAccount(t, store, testId)
			return testId, a.Uuid, account.AccountRoleRequest{RoleName: "new-" + testId}
		}, 400, "invalid external id"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store, testId := setupPostAccountHandlerTest(t)
			userId, uuid, body := c.setup(t, store, testId)
			resp := postRole(t, userId, uuid, body)
			assert.Equal(t, c.status, resp.StatusCode, resp.Body)
			if c.body != "" {
				assert.Contains(t, resp.Body, c.body)
			}
		})
	}
}

// otherAccountSTS assumes any role, but its sessions are in another account.
type otherAccountSTS struct{}

func (otherAccountSTS) AssumeRole(_ context.Context, in *sts.AssumeRoleInput, _ ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	if in.ExternalId == nil {
		return nil, errors.New("AccessDenied")
	}
	return &sts.AssumeRoleOutput{AssumedRoleUser: &types.AssumedRoleUser{
		Arn: aws.String("arn:aws:sts::999999999999:assumed-role/x/pennsieve-account-registration"),
	}}, nil
}

func TestPostAccountRole_RoleInAnotherAccount(t *testing.T) {
	store, testId := setupPostAccountHandlerTest(t)
	useRoleAssumer(t, otherAccountSTS{})
	a := legacyAccount(t, store, testId)
	code := issued(t, externalIdFor(testId), testId, "new-"+testId)
	resp := postRole(t, testId, a.Uuid, account.AccountRoleRequest{RoleName: "new-" + testId, ExternalId: code})
	assert.Equal(t, 400, resp.StatusCode)
	assert.Contains(t, resp.Body, "isn't in this AWS account")
	stored, _ := store.GetById(context.Background(), a.Uuid)
	assert.Equal(t, a.RoleName, stored.RoleName, "the account keeps its role")
}

// A role Pennsieve can't use yet leaves the account and the code as they were.
func TestPostAccountRole_UnusableRoleKeepsTheCode(t *testing.T) {
	store, testId := setupPostAccountHandlerTest(t)
	useRoleAssumer(t, &stsFake{unusable: true})
	account.ShortenVerifyRetries(t)
	a := legacyAccount(t, store, testId)
	code := issued(t, externalIdFor(testId), testId, "new-"+testId)
	resp := postRole(t, testId, a.Uuid, account.AccountRoleRequest{RoleName: "new-" + testId, ExternalId: code})
	assert.Equal(t, 400, resp.StatusCode)
	got, _, err := roleCodes().Get(context.Background(), code)
	require.NoError(t, err)
	assert.False(t, got.Used)
	stored, _ := store.GetById(context.Background(), a.Uuid)
	assert.Equal(t, a.RoleName, stored.RoleName)
}

// A registration from the template is a CloudFormation connection; the
// owner sees it with the published version.
func TestPostAccounts_RecordsTheConnection(t *testing.T) {
	store, testId := setupPostAccountHandlerTest(t)
	t.Setenv("CFN_TEMPLATE_VERSION", "3")
	role := "Pennsieve-Compute-dev-" + testId
	resp := postAccount(t, testId, models.Account{
		AccountId: "acct-" + testId, AccountType: "aws", RoleName: role, Name: "Lab account",
		ExternalId: issued(t, externalIdFor(testId), testId, role), TemplateVersion: "3",
	})
	require.Equal(t, 201, resp.StatusCode, resp.Body)
	var created models.AccountResponse
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &created))
	stored, err := store.GetById(context.Background(), created.Uuid)
	require.NoError(t, err)
	assert.Equal(t, models.ConnectionCloudFormation, stored.Connection)
	assert.Equal(t, "3", stored.TemplateVersion)
	assert.Equal(t, "Lab account", stored.Name, "the name the app sends is stored")

	got, err := account.GetAccountHandler(context.Background(), events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": created.Uuid},
		RequestContext: events.APIGatewayV2HTTPRequestContext{Authorizer: test.CreateTestAuthorizer(testId, "")},
	})
	require.NoError(t, err)
	require.Equal(t, 200, got.StatusCode, got.Body)
	var a models.Account
	require.NoError(t, json.Unmarshal([]byte(got.Body), &a))
	assert.Equal(t, "cloudformation", a.Connection)
	assert.Equal(t, "3", a.TemplateVersion)
	assert.Equal(t, "3", a.LatestTemplateVersion)
}

func TestPostAccounts_InvalidTemplateVersion(t *testing.T) {
	_, testId := setupPostAccountHandlerTest(t)
	resp := postAccount(t, testId, models.Account{AccountId: "acct-" + testId, AccountType: "aws", RoleName: "r", TemplateVersion: "v3"})
	assert.Equal(t, 400, resp.StatusCode)
	assert.Contains(t, resp.Body, "invalid template version")
}
