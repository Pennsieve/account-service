package compute

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createAccountOwnerTestRequest(userId string, accountOwnerMode bool) events.APIGatewayV2HTTPRequest {
	request := events.APIGatewayV2HTTPRequest{
		RouteKey: "GET /compute-nodes",
		RawPath:  "/compute-nodes",
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
				Method: "GET",
			},
			Authorizer: &events.APIGatewayV2HTTPRequestContextAuthorizerDescription{
				Lambda: map[string]interface{}{
					"user_claim": map[string]interface{}{
						"Id":           float64(123),
						"NodeId":       userId,
						"IsSuperAdmin": false,
					},
					"org_claim": map[string]interface{}{
						"NodeId": "org-456",
						"IntId":  float64(456),
						"Role":   float64(16), // Admin role as numeric value
					},
					"iat": float64(1640995200),
					"exp": float64(1640998800),
				},
			},
		},
	}

	if accountOwnerMode {
		request.QueryStringParameters = map[string]string{
			"account_owner": "true",
		}
	}

	return request
}

func TestGetComputesNodesHandler_AccountOwnerMode_Success(t *testing.T) {
	client, err := setupTestClient()
	require.NoError(t, err)

	err = createTestTables(client, "AccountOwner_Success")
	require.NoError(t, err)

	os.Setenv("ENV", "TEST")
	defer func() {
		os.Unsetenv("ENV")
		os.Unsetenv("COMPUTE_NODES_TABLE")
		os.Unsetenv("NODE_ACCESS_TABLE")
		os.Unsetenv("ACCOUNTS_TABLE")
	}()

	// Create test data
	nodeStore := store_dynamodb.NewNodeDatabaseStore(client, os.Getenv("COMPUTE_NODES_TABLE"))
	accountStore := store_dynamodb.NewAccountDatabaseStore(client, os.Getenv("ACCOUNTS_TABLE"))

	userId := "user-123"
	accountUuid := "account-456"

	// Create an account owned by the user
	account := store_dynamodb.Account{
		Uuid:        accountUuid,
		UserId:      userId,
		AccountId:   "aws-account-123456789",
		AccountType: "aws",
		RoleName:    "PennsieveComputeRole",
		ExternalId:  "external-id-123",
		Name:        "Test Account",
		Description: "Test account for account owner mode",
		Status:      "Enabled",
	}
	err = accountStore.Insert(context.Background(), account)
	require.NoError(t, err)

	// Create nodes on this account (some organization-independent, some workspace-scoped)
	testNodes := []models.DynamoDBNode{
		{
			Uuid:           "node-1",
			Name:           "Node 1",
			UserId:         userId,
			OrganizationId: "", // Organization-independent
			AccountUuid:    accountUuid,
			Status:         "Enabled",
			CreatedAt:      time.Now().Format(time.RFC3339),
		},
		{
			Uuid:           "node-2",
			Name:           "Node 2",
			UserId:         "other-user",
			OrganizationId: "org-789", // Different workspace
			AccountUuid:    accountUuid,
			Status:         "Enabled",
			CreatedAt:      time.Now().Format(time.RFC3339),
		},
		{
			Uuid:           "node-3",
			Name:           "Node 3",
			UserId:         "another-user",
			OrganizationId: "org-456", // User's workspace from JWT
			AccountUuid:    accountUuid,
			Status:         "Paused",
			CreatedAt:      time.Now().Format(time.RFC3339),
		},
	}

	for _, node := range testNodes {
		err = nodeStore.Put(context.Background(), node)
		require.NoError(t, err)
	}

	// Create a node on a different account (should NOT be returned)
	differentAccountNode := models.DynamoDBNode{
		Uuid:           "node-different",
		Name:           "Different Account Node",
		UserId:         userId,
		OrganizationId: "",
		AccountUuid:    "different-account-uuid",
		Status:         "Enabled",
		CreatedAt:      time.Now().Format(time.RFC3339),
	}
	err = nodeStore.Put(context.Background(), differentAccountNode)
	require.NoError(t, err)

	// Test account owner mode
	request := createAccountOwnerTestRequest(userId, true)

	response, err := GetComputesNodesHandler(context.Background(), request)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, response.StatusCode)

	var nodes []map[string]interface{}
	err = json.Unmarshal([]byte(response.Body), &nodes)
	require.NoError(t, err)

	// Should return ALL 3 nodes from the user's account, regardless of organization or workspace permissions
	assert.Len(t, nodes, 3, "Should return all 3 nodes from user's account")

	// Verify we got the right nodes (all should be from the user's account)
	nodeUuids := make(map[string]bool)
	for _, node := range nodes {
		nodeUuid, exists := node["uuid"].(string)
		require.True(t, exists, "Node should have uuid field")
		nodeUuids[nodeUuid] = true

		// Verify all nodes are from the user's account
		account := node["account"].(map[string]interface{})
		accountUuidFromResponse := account["uuid"].(string)
		assert.Equal(t, accountUuid, accountUuidFromResponse, "All nodes should be from user's account")
	}

	// Verify we got exactly the expected nodes
	expectedUuids := []string{"node-1", "node-2", "node-3"}
	for _, expectedUuid := range expectedUuids {
		assert.True(t, nodeUuids[expectedUuid], "Should include node %s", expectedUuid)
	}

	// Verify we did NOT get the node from the different account
	assert.False(t, nodeUuids["node-different"], "Should NOT include node from different account")
}

func TestGetComputesNodesHandler_AccountOwnerMode_NotAnAccountOwner(t *testing.T) {
	client, err := setupTestClient()
	require.NoError(t, err)

	err = createTestTables(client, "AccountOwner_NotOwner")
	require.NoError(t, err)

	os.Setenv("ENV", "TEST")
	defer func() {
		os.Unsetenv("ENV")
		os.Unsetenv("COMPUTE_NODES_TABLE")
		os.Unsetenv("NODE_ACCESS_TABLE")
		os.Unsetenv("ACCOUNTS_TABLE")
	}()

	userId := "user-without-accounts"

	// Test account owner mode for a user who doesn't own any accounts
	request := createAccountOwnerTestRequest(userId, true)

	response, err := GetComputesNodesHandler(context.Background(), request)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, response.StatusCode)

	// Should return forbidden error since user is not an account owner
	var errorResponse map[string]interface{}
	err = json.Unmarshal([]byte(response.Body), &errorResponse)
	require.NoError(t, err)

	message, exists := errorResponse["message"]
	assert.True(t, exists, "Error response should contain message")
	assert.Contains(t, message, "Forbidden", "Should indicate forbidden access")
}

func TestGetComputesNodesHandler_AccountOwnerMode_IgnoresOrganizationId(t *testing.T) {
	client, err := setupTestClient()
	require.NoError(t, err)

	err = createTestTables(client, "AccountOwner_IgnoresOrgId")
	require.NoError(t, err)

	os.Setenv("ENV", "TEST")
	defer func() {
		os.Unsetenv("ENV")
		os.Unsetenv("COMPUTE_NODES_TABLE")
		os.Unsetenv("NODE_ACCESS_TABLE")
		os.Unsetenv("ACCOUNTS_TABLE")
	}()

	// Create test data similar to success test but with organization_id parameter
	nodeStore := store_dynamodb.NewNodeDatabaseStore(client, os.Getenv("COMPUTE_NODES_TABLE"))
	accountStore := store_dynamodb.NewAccountDatabaseStore(client, os.Getenv("ACCOUNTS_TABLE"))

	userId := "user-123"
	accountUuid := "account-456"

	// Create an account owned by the user
	account := store_dynamodb.Account{
		Uuid:        accountUuid,
		UserId:      userId,
		AccountId:   "aws-account-123456789",
		AccountType: "aws",
		RoleName:    "PennsieveComputeRole",
		ExternalId:  "external-id-123",
		Name:        "Test Account",
		Status:      "Enabled",
	}
	err = accountStore.Insert(context.Background(), account)
	require.NoError(t, err)

	// Create nodes in different organizations
	testNodes := []models.DynamoDBNode{
		{
			Uuid:           "node-org-123",
			Name:           "Node in Org 123",
			UserId:         userId,
			OrganizationId: "org-123",
			AccountUuid:    accountUuid,
			Status:         "Enabled",
			CreatedAt:      time.Now().Format(time.RFC3339),
		},
		{
			Uuid:           "node-org-456",
			Name:           "Node in Org 456",
			UserId:         userId,
			OrganizationId: "org-456",
			AccountUuid:    accountUuid,
			Status:         "Enabled",
			CreatedAt:      time.Now().Format(time.RFC3339),
		},
	}

	for _, node := range testNodes {
		err = nodeStore.Put(context.Background(), node)
		require.NoError(t, err)
	}

	// Test account owner mode with organization_id parameter (should be ignored)
	request := createAccountOwnerTestRequest(userId, true)
	request.QueryStringParameters["organization_id"] = "org-123" // This should be ignored

	response, err := GetComputesNodesHandler(context.Background(), request)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, response.StatusCode)

	var nodes []map[string]interface{}
	err = json.Unmarshal([]byte(response.Body), &nodes)
	require.NoError(t, err)

	// Should return ALL nodes from user's account, not just those in org-123
	assert.Len(t, nodes, 2, "Should return all nodes from user's account, ignoring organization_id filter")

	// Verify both organizations are represented
	orgIds := make(map[string]bool)
	for _, node := range nodes {
		orgId, exists := node["organizationId"].(string)
		if exists {
			orgIds[orgId] = true
		}
	}

	assert.True(t, orgIds["org-123"], "Should include node from org-123")
	assert.True(t, orgIds["org-456"], "Should include node from org-456") 
}