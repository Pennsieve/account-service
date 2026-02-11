package compute_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/google/uuid"
	"github.com/pennsieve/account-service/internal/handler/compute"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupPermissionHandlerTest(t *testing.T) (store_dynamodb.NodeStore, store_dynamodb.NodeAccessStore) {
	// Use shared test client
	client := test.GetClient()
	nodesStore := store_dynamodb.NewNodeDatabaseStore(client, TEST_NODES_TABLE)
	nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(client, TEST_ACCESS_TABLE)
	
	// Set environment variables for handler
	os.Setenv("COMPUTE_NODES_TABLE", TEST_NODES_TABLE)
	os.Setenv("NODE_ACCESS_TABLE", TEST_ACCESS_TABLE)
	// Don't override ENV if already set (Docker sets it to DOCKER)
	if os.Getenv("ENV") == "" {
		os.Setenv("ENV", "TEST")  // This triggers test-aware config in LoadAWSConfig
	}
	// Don't override DYNAMODB_URL if already set (Docker sets it to http://dynamodb:8000)
	if os.Getenv("DYNAMODB_URL") == "" {
		os.Setenv("DYNAMODB_URL", "http://localhost:8000")  // Local DynamoDB endpoint for local testing
	}
	
	return nodesStore, nodeAccessStore
}

func TestSetNodeAccessScopeHandler_ValidationErrors(t *testing.T) {
	nodesStore, _ := setupPermissionHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create test node
	testNode := models.DynamoDBNode{
		Uuid:           uuid.New().String(),
		Name:           "Test Node",
		UserId:         "user-" + testId,
		AccountUuid:    uuid.New().String(), // Required for DynamoDB GSI
		OrganizationId: "INDEPENDENT",       // Required for DynamoDB GSI
	}
	err := nodesStore.Put(ctx, testNode)
	require.NoError(t, err)

	testCases := []struct {
		name           string
		nodeId         string
		requestBody    string
		userId         string
		expectedCode   int
		expectedError  string
	}{
		{
			name:          "MissingNodeId",
			nodeId:        "",
			requestBody:   `{"accessScope":"workspace"}`,
			userId:        testNode.UserId,
			expectedCode:  400,
			expectedError: "missing node uuid",
		},
		{
			name:          "InvalidJSON",
			nodeId:        testNode.Uuid,
			requestBody:   "{invalid json}",
			userId:        testNode.UserId,
			expectedCode:  400,
			expectedError: "error unmarshaling",
		},
		{
			name:          "InvalidAccessScope",
			nodeId:        testNode.Uuid,
			requestBody:   `{"accessScope":"invalid"}`,
			userId:        testNode.UserId,
			expectedCode:  400,
			expectedError: "invalid access scope",
		},
		{
			name:          "NodeNotFound",
			nodeId:        uuid.New().String(),
			requestBody:   `{"accessScope":"workspace"}`,
			userId:        testNode.UserId,
			expectedCode:  404,
			expectedError: "not found",
		},
		{
			name:          "NotOwner",
			nodeId:        testNode.Uuid,
			requestBody:   `{"accessScope":"workspace"}`,
			userId:        "different-user",
			expectedCode:  403,
			expectedError: "only owner can change permissions",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pathParams := map[string]string{}
			if tc.nodeId != "" {
				pathParams["id"] = tc.nodeId
			}

			request := events.APIGatewayV2HTTPRequest{
				Body:           tc.requestBody,
				PathParameters: pathParams,
				RequestContext: events.APIGatewayV2HTTPRequestContext{
					Authorizer: test.CreateTestAuthorizer(tc.userId, "test-org"),
				},
			}

			response, err := compute.SetNodeAccessScopeHandler(ctx, request)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedCode, response.StatusCode)
			assert.Contains(t, response.Body, tc.expectedError)
		})
	}
}

func TestGrantUserAccessHandler_ValidationErrors(t *testing.T) {
	nodesStore, _ := setupPermissionHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create test node
	testNode := models.DynamoDBNode{
		Uuid:           uuid.New().String(),
		Name:           "Test Node",
		UserId:         "user-" + testId,
		AccountUuid:    uuid.New().String(), // Required for DynamoDB GSI
		OrganizationId: "INDEPENDENT",       // Required for DynamoDB GSI
	}
	err := nodesStore.Put(ctx, testNode)
	require.NoError(t, err)

	testCases := []struct {
		name          string
		nodeId        string
		requestBody   string
		userId        string
		expectedCode  int
		expectedError string
	}{
		{
			name:          "NotOwner",
			nodeId:        testNode.Uuid,
			requestBody:   `{"userId":"grantee"}`,
			userId:        "different-user",
			expectedCode:  403,
			expectedError: "only owner can change permissions",
		},
		{
			name:          "NodeNotFound",
			nodeId:        uuid.New().String(),
			requestBody:   `{"userId":"grantee"}`,
			userId:        testNode.UserId,
			expectedCode:  404,
			expectedError: "not found",
		},
		{
			name:          "InvalidJSON",
			nodeId:        testNode.Uuid,
			requestBody:   "{invalid}",
			userId:        testNode.UserId,
			expectedCode:  400,
			expectedError: "error unmarshaling",
		},
		{
			name:          "MissingUserId",
			nodeId:        testNode.Uuid,
			requestBody:   `{}`,
			userId:        testNode.UserId,
			expectedCode:  400,
			expectedError: "missing user id",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			request := events.APIGatewayV2HTTPRequest{
				Body: tc.requestBody,
				PathParameters: map[string]string{
					"id": tc.nodeId,
				},
				RequestContext: events.APIGatewayV2HTTPRequestContext{
					Authorizer: test.CreateTestAuthorizer(tc.userId, "test-org"),
				},
			}

			response, err := compute.GrantUserAccessHandler(ctx, request)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedCode, response.StatusCode)
			assert.Contains(t, response.Body, tc.expectedError)
		})
	}
}

func TestRevokeUserAccessHandler_ValidationErrors(t *testing.T) {
	nodesStore, _ := setupPermissionHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create test node
	testNode := models.DynamoDBNode{
		Uuid:           uuid.New().String(),
		Name:           "Test Node",
		UserId:         "user-" + testId,
		AccountUuid:    uuid.New().String(), // Required for DynamoDB GSI
		OrganizationId: "INDEPENDENT",       // Required for DynamoDB GSI
	}
	err := nodesStore.Put(ctx, testNode)
	require.NoError(t, err)

	testCases := []struct {
		name          string
		nodeId        string
		targetUserId  string
		userId        string
		expectedCode  int
		expectedError string
	}{
		{
			name:          "NotOwner",
			nodeId:        testNode.Uuid,
			targetUserId:  "grantee",
			userId:        "different-user",
			expectedCode:  403,
			expectedError: "only owner can change permissions",
		},
		{
			name:          "NodeNotFound",
			nodeId:        uuid.New().String(),
			targetUserId:  "grantee",
			userId:        testNode.UserId,
			expectedCode:  404,
			expectedError: "not found",
		},
		{
			name:          "MissingUserId",
			nodeId:        testNode.Uuid,
			targetUserId:  "",
			userId:        testNode.UserId,
			expectedCode:  400,
			expectedError: "missing user id",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pathParams := map[string]string{
				"id": tc.nodeId,
			}
			if tc.targetUserId != "" {
				pathParams["userId"] = tc.targetUserId
			}

			request := events.APIGatewayV2HTTPRequest{
				PathParameters: pathParams,
				RequestContext: events.APIGatewayV2HTTPRequestContext{
					Authorizer: test.CreateTestAuthorizer(tc.userId, "test-org"),
				},
			}

			response, err := compute.RevokeUserAccessHandler(ctx, request)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedCode, response.StatusCode)
			assert.Contains(t, response.Body, tc.expectedError)
		})
	}
}

func TestGrantTeamAccessHandler_ValidationErrors(t *testing.T) {
	nodesStore, _ := setupPermissionHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create test node
	testNode := models.DynamoDBNode{
		Uuid:           uuid.New().String(),
		Name:           "Test Node",
		UserId:         "user-" + testId,
		AccountUuid:    uuid.New().String(), // Required for DynamoDB GSI
		OrganizationId: "INDEPENDENT",       // Required for DynamoDB GSI
	}
	err := nodesStore.Put(ctx, testNode)
	require.NoError(t, err)

	testCases := []struct {
		name          string
		nodeId        string
		requestBody   string
		userId        string
		expectedCode  int
		expectedError string
	}{
		{
			name:          "NotOwner",
			nodeId:        testNode.Uuid,
			requestBody:   `{"teamId":"team1"}`,
			userId:        "different-user",
			expectedCode:  403,
			expectedError: "only owner can change permissions",
		},
		{
			name:          "NodeNotFound",
			nodeId:        uuid.New().String(),
			requestBody:   `{"teamId":"team1"}`,
			userId:        testNode.UserId,
			expectedCode:  404,
			expectedError: "not found",
		},
		{
			name:          "MissingTeamId",
			nodeId:        testNode.Uuid,
			requestBody:   `{}`,
			userId:        testNode.UserId,
			expectedCode:  400,
			expectedError: "missing team id",
		},
		{
			name:          "InvalidJSON",
			nodeId:        testNode.Uuid,
			requestBody:   "{invalid}",
			userId:        testNode.UserId,
			expectedCode:  400,
			expectedError: "error unmarshaling",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			request := events.APIGatewayV2HTTPRequest{
				Body: tc.requestBody,
				PathParameters: map[string]string{
					"id": tc.nodeId,
				},
				RequestContext: events.APIGatewayV2HTTPRequestContext{
					Authorizer: test.CreateTestAuthorizer(tc.userId, "test-org"),
				},
			}

			response, err := compute.GrantTeamAccessHandler(ctx, request)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedCode, response.StatusCode)
			assert.Contains(t, response.Body, tc.expectedError)
		})
	}
}

func TestRevokeTeamAccessHandler_ValidationErrors(t *testing.T) {
	nodesStore, _ := setupPermissionHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create test node
	testNode := models.DynamoDBNode{
		Uuid:           uuid.New().String(),
		Name:           "Test Node",
		UserId:         "user-" + testId,
		AccountUuid:    uuid.New().String(), // Required for DynamoDB GSI
		OrganizationId: "INDEPENDENT",       // Required for DynamoDB GSI
	}
	err := nodesStore.Put(ctx, testNode)
	require.NoError(t, err)

	testCases := []struct {
		name          string
		nodeId        string
		targetTeamId  string
		userId        string
		expectedCode  int
		expectedError string
	}{
		{
			name:          "NotOwner",
			nodeId:        testNode.Uuid,
			targetTeamId:  "team1",
			userId:        "different-user",
			expectedCode:  403,
			expectedError: "only owner can change permissions",
		},
		{
			name:          "NodeNotFound",
			nodeId:        uuid.New().String(),
			targetTeamId:  "team1",
			userId:        testNode.UserId,
			expectedCode:  404,
			expectedError: "not found",
		},
		{
			name:          "MissingTeamId",
			nodeId:        testNode.Uuid,
			targetTeamId:  "",
			userId:        testNode.UserId,
			expectedCode:  400,
			expectedError: "missing team id",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pathParams := map[string]string{
				"id": tc.nodeId,
			}
			if tc.targetTeamId != "" {
				pathParams["teamId"] = tc.targetTeamId
			}

			request := events.APIGatewayV2HTTPRequest{
				PathParameters: pathParams,
				RequestContext: events.APIGatewayV2HTTPRequestContext{
					Authorizer: test.CreateTestAuthorizer(tc.userId, "test-org"),
				},
			}

			response, err := compute.RevokeTeamAccessHandler(ctx, request)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedCode, response.StatusCode)
			assert.Contains(t, response.Body, tc.expectedError)
		})
	}
}

func TestPermissionHandlers_MissingEnvironmentVariables(t *testing.T) {
	ctx := context.Background()
	testId := test.GenerateTestId()

	testCases := []struct {
		name         string
		envToUnset   string
		expectedCode int
		expectedErr  string
	}{
		{
			name:         "MissingNodesTable",
			envToUnset:   "COMPUTE_NODES_TABLE",
			expectedCode: 500,
			expectedErr:  "error loading AWS config",
		},
		{
			name:         "MissingAccessTable", 
			envToUnset:   "NODE_ACCESS_TABLE",
			expectedCode: 500,
			expectedErr:  "error loading AWS config",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup environment but remove the specific variable
			setupPermissionHandlerTest(t)
			originalValue := os.Getenv(tc.envToUnset)
			_ = os.Unsetenv(tc.envToUnset)
			defer func() { _ = os.Setenv(tc.envToUnset, originalValue) }()

			requestBody, err := json.Marshal(map[string]string{
				"accessScope": "workspace",
			})
			require.NoError(t, err)

			request := events.APIGatewayV2HTTPRequest{
				Body: string(requestBody),
				PathParameters: map[string]string{
					"id": uuid.New().String(),
				},
				RequestContext: events.APIGatewayV2HTTPRequestContext{
					Authorizer: test.CreateTestAuthorizer("user-"+testId, "org-"+testId),
				},
			}

			response, err := compute.SetNodeAccessScopeHandler(ctx, request)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedCode, response.StatusCode)
			assert.Contains(t, response.Body, tc.expectedErr)
		})
	}
}