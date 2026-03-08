package compute_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/account-service/internal/handler/compute"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupSecretsHandlerTest(t *testing.T) (store_dynamodb.NodeStore, store_dynamodb.NodeAccessStore) {
	client := test.GetClient()
	nodeStore := store_dynamodb.NewNodeDatabaseStore(client, TEST_NODES_TABLE)
	nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(client, TEST_ACCESS_TABLE)

	os.Setenv("COMPUTE_NODES_TABLE", TEST_NODES_TABLE)
	os.Setenv("NODE_ACCESS_TABLE", TEST_ACCESS_TABLE)
	if os.Getenv("ENV") == "" {
		os.Setenv("ENV", "TEST")
	}
	if os.Getenv("DYNAMODB_URL") == "" {
		os.Setenv("DYNAMODB_URL", "http://localhost:8000")
	}

	return nodeStore, nodeAccessStore
}

// --- Missing node ID tests (all handlers) ---

func TestPutSecretsHandler_MissingNodeId(t *testing.T) {
	_, _ = setupSecretsHandlerTest(t)
	ctx := context.Background()

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{},
		Body:           `{"secrets":{"key":"value"}}`,
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("test-user", "test-org"),
		},
	}

	response, err := compute.PutSecretsHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.Contains(t, response.Body, "missing node uuid")
}

func TestGetSecretsHandler_MissingNodeId(t *testing.T) {
	_, _ = setupSecretsHandlerTest(t)
	ctx := context.Background()

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("test-user", "test-org"),
		},
	}

	response, err := compute.GetSecretsHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.Contains(t, response.Body, "missing node uuid")
}

func TestDeleteSecretsHandler_MissingNodeId(t *testing.T) {
	_, _ = setupSecretsHandlerTest(t)
	ctx := context.Background()

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("test-user", "test-org"),
		},
	}

	response, err := compute.DeleteSecretsHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.Contains(t, response.Body, "missing node uuid")
}

func TestPutSharedSecretsHandler_MissingNodeId(t *testing.T) {
	_, _ = setupSecretsHandlerTest(t)
	ctx := context.Background()

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{},
		Body:           `{"secrets":{"key":"value"}}`,
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("test-user", "test-org"),
		},
	}

	response, err := compute.PutSharedSecretsHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.Contains(t, response.Body, "missing node uuid")
}

func TestGetSharedSecretsHandler_MissingNodeId(t *testing.T) {
	_, _ = setupSecretsHandlerTest(t)
	ctx := context.Background()

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("test-user", "test-org"),
		},
	}

	response, err := compute.GetSharedSecretsHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.Contains(t, response.Body, "missing node uuid")
}

func TestDeleteSharedSecretsHandler_MissingNodeId(t *testing.T) {
	_, _ = setupSecretsHandlerTest(t)
	ctx := context.Background()

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("test-user", "test-org"),
		},
	}

	response, err := compute.DeleteSharedSecretsHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.Contains(t, response.Body, "missing node uuid")
}

// --- Shared secrets: owner-only access control ---

func TestPutSharedSecretsHandler_NonOwnerForbidden(t *testing.T) {
	nodeStore, _ := setupSecretsHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	testNode := createTestNode(testId)
	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]interface{}{"secrets": map[string]string{"key": "value"}})

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": testNode.Uuid},
		Body:           string(body),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("different-user", testNode.OrganizationId),
		},
	}

	response, err := compute.PutSharedSecretsHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, response.StatusCode)
	assert.Contains(t, response.Body, "only owner can change permissions")
}

func TestDeleteSharedSecretsHandler_NonOwnerForbidden(t *testing.T) {
	nodeStore, _ := setupSecretsHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	testNode := createTestNode(testId)
	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": testNode.Uuid},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("different-user", testNode.OrganizationId),
		},
	}

	response, err := compute.DeleteSharedSecretsHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, response.StatusCode)
	assert.Contains(t, response.Body, "only owner can change permissions")
}

// --- Validation tests via shared secrets (owner path, no Postgres needed) ---

func TestPutSharedSecretsHandler_InvalidJSON(t *testing.T) {
	nodeStore, _ := setupSecretsHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	testNode := createTestNode(testId)
	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": testNode.Uuid},
		Body:           "{bad json",
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
		},
	}

	response, err := compute.PutSharedSecretsHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.Contains(t, response.Body, "error unmarshaling")
}

func TestPutSharedSecretsHandler_EmptySecrets(t *testing.T) {
	nodeStore, _ := setupSecretsHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	testNode := createTestNode(testId)
	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": testNode.Uuid},
		Body:           `{"secrets":{}}`,
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
		},
	}

	response, err := compute.PutSharedSecretsHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.Contains(t, response.Body, "bad request")
}

func TestPutSharedSecretsHandler_TooManySecrets(t *testing.T) {
	nodeStore, _ := setupSecretsHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	testNode := createTestNode(testId)
	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	secrets := make(map[string]string)
	for i := 0; i < 51; i++ {
		secrets[fmt.Sprintf("key-%d", i)] = "value"
	}
	body, _ := json.Marshal(map[string]interface{}{"secrets": secrets})

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": testNode.Uuid},
		Body:           string(body),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
		},
	}

	response, err := compute.PutSharedSecretsHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.Contains(t, response.Body, "too many secrets")
}

func TestPutSharedSecretsHandler_SecretKeyTooLong(t *testing.T) {
	nodeStore, _ := setupSecretsHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	testNode := createTestNode(testId)
	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	longKey := strings.Repeat("k", 257)
	body, _ := json.Marshal(map[string]interface{}{"secrets": map[string]string{longKey: "value"}})

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": testNode.Uuid},
		Body:           string(body),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
		},
	}

	response, err := compute.PutSharedSecretsHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.Contains(t, response.Body, "secret key exceeds max length")
}

func TestPutSharedSecretsHandler_SecretValueTooLong(t *testing.T) {
	nodeStore, _ := setupSecretsHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	testNode := createTestNode(testId)
	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	longValue := strings.Repeat("v", 10001)
	body, _ := json.Marshal(map[string]interface{}{"secrets": map[string]string{"key": longValue}})

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": testNode.Uuid},
		Body:           string(body),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
		},
	}

	response, err := compute.PutSharedSecretsHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.Contains(t, response.Body, "secret value exceeds max length")
}

// --- Node without gateway URL ---

func TestPutSharedSecretsHandler_NodeWithoutGatewayUrl(t *testing.T) {
	nodeStore, _ := setupSecretsHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	testNode := createTestNode(testId)
	testNode.ComputeNodeGatewayUrl = ""
	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]interface{}{"secrets": map[string]string{"key": "value"}})

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": testNode.Uuid},
		Body:           string(body),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
		},
	}

	response, err := compute.PutSharedSecretsHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.Contains(t, response.Body, "bad request")
}

// --- Node not found ---

func TestPutSharedSecretsHandler_NodeNotFound(t *testing.T) {
	_, _ = setupSecretsHandlerTest(t)
	ctx := context.Background()

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": "non-existent-node-id"},
		Body:           `{"secrets":{"key":"value"}}`,
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("test-user", "test-org"),
		},
	}

	response, err := compute.PutSharedSecretsHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, response.StatusCode)
	assert.Contains(t, response.Body, "not found")
}
