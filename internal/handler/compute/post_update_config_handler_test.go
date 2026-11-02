package compute_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/google/uuid"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/handler/compute"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupPostUpdateConfigHandlerTest(t *testing.T) (store_dynamodb.NodeStore, store_dynamodb.DynamoDBStore) {
	client := test.GetClient()
	nodeStore := store_dynamodb.NewNodeDatabaseStore(client, TEST_NODES_TABLE)
	accountStore := store_dynamodb.NewAccountDatabaseStore(client, test.TEST_ACCOUNTS_WITH_INDEX_TABLE)

	os.Setenv("COMPUTE_NODES_TABLE", TEST_NODES_TABLE)
	os.Setenv("ACCOUNTS_TABLE", test.TEST_ACCOUNTS_WITH_INDEX_TABLE)
	os.Setenv("NODE_ACCESS_TABLE", TEST_ACCESS_TABLE)
	os.Setenv("ACCOUNT_WORKSPACE_TABLE", test.TEST_WORKSPACE_TABLE)
	// Don't override ENV if already set (Docker sets it to DOCKER)
	if os.Getenv("ENV") == "" {
		os.Setenv("ENV", "TEST")
	}
	// Don't override DYNAMODB_URL if already set (Docker sets it to http://dynamodb:8000)
	if os.Getenv("DYNAMODB_URL") == "" {
		os.Setenv("DYNAMODB_URL", "http://localhost:8000")
	}

	return nodeStore, accountStore
}

func postUpdateConfig(t *testing.T, node models.DynamoDBNode, userId string, body map[string]any) events.APIGatewayV2HTTPResponse {
	requestBody, err := json.Marshal(body)
	require.NoError(t, err)

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": node.Uuid,
		},
		Body: string(requestBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, node.OrganizationId),
		},
	}

	response, err := compute.PostUpdateConfigHandler(context.Background(), request)
	require.NoError(t, err)
	return response
}

func TestPostUpdateConfigHandler_LLMBaaCheck(t *testing.T) {
	nodeStore, accountStore := setupPostUpdateConfigHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	seedNode := func(t *testing.T, suffix, mode string, llmOn bool) models.DynamoDBNode {
		node := createTestNode(testId + "_" + suffix)
		node.DeploymentMode = mode
		node.EnableLLMAccess = llmOn
		node.GpuTier = "small"
		node.MaxGpuInstances = 1
		require.NoError(t, nodeStore.Put(ctx, node))
		return node
	}

	for _, mode := range []string{"secure", "compliant"} {
		t.Run("OffToOn_NoBaa_Rejected_"+mode, func(t *testing.T) {
			node := seedNode(t, "reject_"+mode, mode, false)

			response := postUpdateConfig(t, node, node.UserId, map[string]any{
				"enableLLMAccess": true,
				"gpuTier":         "large",
			})

			assert.Equal(t, 400, response.StatusCode)
			assert.Equal(t, errors.ComputeHandlerError("PostUpdateConfigHandler", errors.ErrLLMBaaRequired), response.Body)

			stored, err := nodeStore.GetById(ctx, node.Uuid)
			require.NoError(t, err)
			assert.False(t, stored.EnableLLMAccess)
			assert.False(t, stored.LlmBaaAcknowledged)
			assert.Equal(t, "small", stored.GpuTier)
		})
	}

	t.Run("OffToOn_WithBaa_Accepted", func(t *testing.T) {
		node := seedNode(t, "baa", "secure", false)

		response := postUpdateConfig(t, node, node.UserId, map[string]any{
			"enableLLMAccess":    true,
			"llmBaaAcknowledged": true,
		})

		assert.Equal(t, 202, response.StatusCode)

		stored, err := nodeStore.GetById(ctx, node.Uuid)
		require.NoError(t, err)
		assert.True(t, stored.EnableLLMAccess)
		assert.True(t, stored.LlmBaaAcknowledged)
	})

	t.Run("OffToOn_StoredBaa_Accepted", func(t *testing.T) {
		node := createTestNode(testId + "_storedbaa")
		node.DeploymentMode = "secure"
		node.LlmBaaAcknowledged = true
		require.NoError(t, nodeStore.Put(ctx, node))

		response := postUpdateConfig(t, node, node.UserId, map[string]any{
			"enableLLMAccess": true,
		})

		assert.Equal(t, 202, response.StatusCode)

		stored, err := nodeStore.GetById(ctx, node.Uuid)
		require.NoError(t, err)
		assert.True(t, stored.EnableLLMAccess)
		assert.True(t, stored.LlmBaaAcknowledged)
	})

	t.Run("ExplicitFalseBaa_DoesNotClearStoredFlag", func(t *testing.T) {
		node := createTestNode(testId + "_falsebaa")
		node.DeploymentMode = "secure"
		node.LlmBaaAcknowledged = true
		require.NoError(t, nodeStore.Put(ctx, node))

		response := postUpdateConfig(t, node, node.UserId, map[string]any{
			"enableLLMAccess":    true,
			"llmBaaAcknowledged": false,
		})

		assert.Equal(t, 202, response.StatusCode)

		stored, err := nodeStore.GetById(ctx, node.Uuid)
		require.NoError(t, err)
		assert.True(t, stored.EnableLLMAccess)
		assert.True(t, stored.LlmBaaAcknowledged)
	})

	t.Run("AlreadyOn_ResendWithGpuChange_Accepted", func(t *testing.T) {
		node := seedNode(t, "resend", "secure", true)

		response := postUpdateConfig(t, node, node.UserId, map[string]any{
			"enableLLMAccess": true,
			"gpuTier":         "large",
		})

		assert.Equal(t, 202, response.StatusCode)

		stored, err := nodeStore.GetById(ctx, node.Uuid)
		require.NoError(t, err)
		assert.Equal(t, "large", stored.GpuTier)
		assert.False(t, stored.LlmBaaAcknowledged)
	})

	t.Run("BasicMode_OffToOn_NoBaa_Accepted", func(t *testing.T) {
		node := seedNode(t, "basic", "basic", false)

		response := postUpdateConfig(t, node, node.UserId, map[string]any{
			"enableLLMAccess": true,
		})

		assert.Equal(t, 202, response.StatusCode)

		stored, err := nodeStore.GetById(ctx, node.Uuid)
		require.NoError(t, err)
		assert.True(t, stored.EnableLLMAccess)
	})

	t.Run("NoAccess_GetsForbiddenNotBaaError", func(t *testing.T) {
		testAccount := store_dynamodb.Account{
			Uuid:        uuid.New().String(),
			AccountId:   "test-account-" + testId + "_forbidden",
			AccountType: "aws",
			UserId:      test.UserNodeId("account-owner-" + testId),
		}
		require.NoError(t, accountStore.Insert(ctx, testAccount))

		node := createTestNode(testId + "_forbidden")
		node.AccountUuid = testAccount.Uuid
		node.DeploymentMode = "secure"
		require.NoError(t, nodeStore.Put(ctx, node))

		response := postUpdateConfig(t, node, test.UserNodeId("random-user-"+testId), map[string]any{
			"enableLLMAccess": true,
		})

		assert.Equal(t, 403, response.StatusCode)
		assert.Contains(t, response.Body, "forbidden")
	})
}
