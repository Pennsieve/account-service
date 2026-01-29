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

func setupDeleteComputeNodeHandlerTest(t *testing.T) (store_dynamodb.NodeStore, store_dynamodb.DynamoDBStore) {
	// Use shared test client
	client := test.GetClient()
	nodeStore := store_dynamodb.NewNodeDatabaseStore(client, TEST_NODES_TABLE)
	accountStore := store_dynamodb.NewAccountDatabaseStore(client, test.TEST_ACCOUNTS_WITH_INDEX_TABLE)
	
	// Set environment variables for handler
	os.Setenv("COMPUTE_NODES_TABLE", TEST_NODES_TABLE)
	os.Setenv("ACCOUNTS_TABLE", test.TEST_ACCOUNTS_WITH_INDEX_TABLE)
	os.Setenv("TASK_DEF_ARN", "arn:aws:ecs:us-east-1:123456789012:task-definition/test-task:1")
	os.Setenv("SUBNET_IDS", "subnet-12345,subnet-67890")
	os.Setenv("CLUSTER_ARN", "arn:aws:ecs:us-east-1:123456789012:cluster/test-cluster")
	os.Setenv("SECURITY_GROUP", "sg-12345")
	os.Setenv("TASK_DEF_CONTAINER_NAME", "test-container")
	// Don't override ENV if already set (Docker sets it to DOCKER)
	if os.Getenv("ENV") == "" {
		os.Setenv("ENV", "TEST")  // This triggers test-aware config in LoadAWSConfig
	}
	// Don't override DYNAMODB_URL if already set (Docker sets it to http://dynamodb:8000)
	if os.Getenv("DYNAMODB_URL") == "" {
		os.Setenv("DYNAMODB_URL", "http://localhost:8000")  // Local DynamoDB endpoint for local testing
	}
	
	return nodeStore, accountStore
}

func TestDeleteComputeNodeHandler_Success(t *testing.T) {
	nodeStore, _ := setupDeleteComputeNodeHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create and insert test node
	testNode := createTestNode(testId)
	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	// Create request with test authorizer
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testNode.Uuid,
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
		},
	}

	// Call the handler
	response, err := compute.DeleteComputeNodeHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, 202, response.StatusCode) // Accepted (async operation)

	// Parse response body
	var nodeResponse models.NodeResponse
	err = json.Unmarshal([]byte(response.Body), &nodeResponse)
	assert.NoError(t, err)
	assert.Equal(t, "Compute node deletion initiated", nodeResponse.Message)
}

func TestDeleteComputeNodeHandler_NotFound(t *testing.T) {
	_, _ = setupDeleteComputeNodeHandlerTest(t)
	ctx := context.Background()

	nonExistentId := uuid.New().String()

	// Create request for non-existent node with test authorizer
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": nonExistentId,
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("test-user", "test-org"),
		},
	}

	// Call the handler
	response, err := compute.DeleteComputeNodeHandler(ctx, request)
	assert.NoError(t, err)

	// Should return 404 Not Found
	assert.Equal(t, 404, response.StatusCode)
	assert.Contains(t, response.Body, "no records found")
}

func TestDeleteComputeNodeHandler_MissingId(t *testing.T) {
	_, _ = setupDeleteComputeNodeHandlerTest(t)
	ctx := context.Background()

	// Create request without ID parameter but with test authorizer
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("test-user", "test-org"),
		},
	}

	// Call the handler
	response, err := compute.DeleteComputeNodeHandler(ctx, request)
	assert.NoError(t, err)

	// When ID is missing, DynamoDB returns an error for empty string key
	// This should result in a 500 Internal Server Error
	assert.Equal(t, 500, response.StatusCode)
	assert.Contains(t, response.Body, "error performing action on DynamoDB table")
}

func TestDeleteComputeNodeHandler_DifferentNodeStatuses(t *testing.T) {
	nodeStore, _ := setupDeleteComputeNodeHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Test deletion of nodes with different statuses
	testCases := []struct {
		name   string
		status string
	}{
		{"Enabled Node", "Enabled"},
		{"Paused Node", "Paused"},
		{"Provisioning Node", "Provisioning"},
		{"Failed Node", "Failed"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create node with specific status
			testNode := createTestNode(testId)
			testNode.Uuid = uuid.New().String() // Unique UUID for each test case
			testNode.Status = tc.status
			testNode.Name = tc.name

			err := nodeStore.Put(ctx, testNode)
			require.NoError(t, err)

			// Request to delete the node with test authorizer
			request := events.APIGatewayV2HTTPRequest{
				PathParameters: map[string]string{
					"id": testNode.Uuid,
				},
				RequestContext: events.APIGatewayV2HTTPRequestContext{
					Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
				},
			}

			response, err := compute.DeleteComputeNodeHandler(ctx, request)
			assert.NoError(t, err)
			
			// Should successfully initiate deletion regardless of status
			assert.Equal(t, 202, response.StatusCode)

			var nodeResponse models.NodeResponse
			err = json.Unmarshal([]byte(response.Body), &nodeResponse)
			assert.NoError(t, err)
			assert.Equal(t, "Compute node deletion initiated", nodeResponse.Message)
		})
	}
}

func TestDeleteComputeNodeHandler_MissingRequiredEnvVars(t *testing.T) {
	_, _ = setupDeleteComputeNodeHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Test missing TASK_DEF_ARN
	t.Run("Missing TASK_DEF_ARN", func(t *testing.T) {
		// Temporarily unset the environment variable
		originalValue := os.Getenv("TASK_DEF_ARN")
		_ = os.Unsetenv("TASK_DEF_ARN")
		defer func() { _ = os.Setenv("TASK_DEF_ARN", originalValue) }()

		// Create and insert test node (even though ECS will fail, we should still fetch the node)
		nodeStore, _ := setupDeleteComputeNodeHandlerTest(t)
		testNode := createTestNode(testId)
		err := nodeStore.Put(ctx, testNode)
		require.NoError(t, err)

		request := events.APIGatewayV2HTTPRequest{
			PathParameters: map[string]string{
				"id": testNode.Uuid,
			},
			RequestContext: events.APIGatewayV2HTTPRequestContext{
				Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
			},
		}

		response, err := compute.DeleteComputeNodeHandler(ctx, request)
		assert.NoError(t, err)

		// In test environment, ECS execution is skipped, so missing env vars don't cause failure
		// Should return 202 Accepted with success message
		assert.Equal(t, 202, response.StatusCode)
		assert.Contains(t, response.Body, "Compute node deletion initiated")
	})
}

func TestDeleteComputeNodeHandler_MissingComputeNodesTable(t *testing.T) {
	_, _ = setupDeleteComputeNodeHandlerTest(t)
	ctx := context.Background()

	// Unset the COMPUTE_NODES_TABLE environment variable
	_ = os.Unsetenv("COMPUTE_NODES_TABLE")
	defer func() { _ = os.Setenv("COMPUTE_NODES_TABLE", TEST_NODES_TABLE) }() // Restore after test

	testId := uuid.New().String()

	// Create request
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testId,
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("test-user", "test-org"),
		},
	}

	// Call the handler
	response, err := compute.DeleteComputeNodeHandler(ctx, request)
	assert.NoError(t, err)

	// Should return 500 because DynamoDB returns an error for invalid table name
	assert.Equal(t, 500, response.StatusCode)
	assert.Contains(t, response.Body, "error performing action on DynamoDB table")
}

func TestDeleteComputeNodeHandler_OrganizationIndependentNode(t *testing.T) {
	nodeStore, _ := setupDeleteComputeNodeHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create organization-independent node
	testNode := createTestNode(testId)
	testNode.OrganizationId = "INDEPENDENT" // Organization-independent node

	err := nodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	// Create request with test authorizer (no organization claim)
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testNode.Uuid,
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, ""), // No org claim
		},
	}

	// Call the handler
	response, err := compute.DeleteComputeNodeHandler(ctx, request)
	assert.NoError(t, err)

	// Should successfully initiate deletion
	assert.Equal(t, 202, response.StatusCode)

	// Parse response body
	var nodeResponse models.NodeResponse
	err = json.Unmarshal([]byte(response.Body), &nodeResponse)
	assert.NoError(t, err)
	assert.Equal(t, "Compute node deletion initiated", nodeResponse.Message)
}

func TestDeleteComputeNodeHandler_DifferentAccountTypes(t *testing.T) {
	nodeStore, _ := setupDeleteComputeNodeHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Test deletion of nodes with different account types
	testCases := []struct {
		name        string
		accountType string
	}{
		{"AWS Account", "aws"},
		{"Azure Account", "azure"},
		{"GCP Account", "gcp"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create node with specific account type
			testNode := createTestNode(testId)
			testNode.Uuid = uuid.New().String() // Unique UUID for each test case
			testNode.AccountType = tc.accountType
			testNode.Name = tc.name

			err := nodeStore.Put(ctx, testNode)
			require.NoError(t, err)

			// Request to delete the node
			request := events.APIGatewayV2HTTPRequest{
				PathParameters: map[string]string{
					"id": testNode.Uuid,
				},
				RequestContext: events.APIGatewayV2HTTPRequestContext{
					Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
				},
			}

			response, err := compute.DeleteComputeNodeHandler(ctx, request)
			assert.NoError(t, err)
			
			// Should successfully initiate deletion regardless of account type
			assert.Equal(t, 202, response.StatusCode)

			var nodeResponse models.NodeResponse
			err = json.Unmarshal([]byte(response.Body), &nodeResponse)
			assert.NoError(t, err)
			assert.Equal(t, "Compute node deletion initiated", nodeResponse.Message)
		})
	}
}

func TestDeleteComputeNodeHandler_PermissionChecks(t *testing.T) {
	nodeStore, accountStore := setupDeleteComputeNodeHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Test case 1: Node owner can delete
	t.Run("NodeOwner_CanDelete", func(t *testing.T) {
		testNode := createTestNode(testId + "_owner")
		nodeOwner := testNode.UserId
		
		err := nodeStore.Put(ctx, testNode)
		require.NoError(t, err)

		request := events.APIGatewayV2HTTPRequest{
			PathParameters: map[string]string{
				"id": testNode.Uuid,
			},
			RequestContext: events.APIGatewayV2HTTPRequestContext{
				Authorizer: test.CreateTestAuthorizer(nodeOwner, testNode.OrganizationId),
			},
		}

		response, err := compute.DeleteComputeNodeHandler(ctx, request)
		assert.NoError(t, err)
		assert.Equal(t, 202, response.StatusCode)
		assert.Contains(t, response.Body, "Compute node deletion initiated")
	})

	// Test case 2: Account owner can delete
	t.Run("AccountOwner_CanDelete", func(t *testing.T) {
		// Create test account
		testAccount := store_dynamodb.Account{
			Uuid:        uuid.New().String(),
			AccountId:   "test-account-" + testId,
			AccountType: "aws",
			UserId:      "account-owner-" + testId,
		}
		err := accountStore.Insert(ctx, testAccount)
		require.NoError(t, err)

		// Create node owned by different user but using the test account
		testNode := createTestNode(testId + "_account")
		testNode.AccountUuid = testAccount.Uuid
		testNode.UserId = "different-user-" + testId // Different from account owner
		
		err = nodeStore.Put(ctx, testNode)
		require.NoError(t, err)

		// Request from account owner (not node owner)
		request := events.APIGatewayV2HTTPRequest{
			PathParameters: map[string]string{
				"id": testNode.Uuid,
			},
			RequestContext: events.APIGatewayV2HTTPRequestContext{
				Authorizer: test.CreateTestAuthorizer(testAccount.UserId, testNode.OrganizationId),
			},
		}

		response, err := compute.DeleteComputeNodeHandler(ctx, request)
		assert.NoError(t, err)
		assert.Equal(t, 202, response.StatusCode)
		assert.Contains(t, response.Body, "Compute node deletion initiated")
	})

	// Test case 3: Non-owner, non-account-owner cannot delete
	t.Run("RandomUser_CannotDelete", func(t *testing.T) {
		// Create test account
		testAccount := store_dynamodb.Account{
			Uuid:        uuid.New().String(),
			AccountId:   "test-account-" + testId + "_forbidden",
			AccountType: "aws",
			UserId:      "account-owner-" + testId + "_forbidden",
		}
		err := accountStore.Insert(ctx, testAccount)
		require.NoError(t, err)

		// Create node
		testNode := createTestNode(testId + "_forbidden")
		testNode.AccountUuid = testAccount.Uuid
		
		err = nodeStore.Put(ctx, testNode)
		require.NoError(t, err)

		// Request from random user (neither node owner nor account owner)
		randomUserId := "random-user-" + testId
		request := events.APIGatewayV2HTTPRequest{
			PathParameters: map[string]string{
				"id": testNode.Uuid,
			},
			RequestContext: events.APIGatewayV2HTTPRequestContext{
				Authorizer: test.CreateTestAuthorizer(randomUserId, testNode.OrganizationId),
			},
		}

		response, err := compute.DeleteComputeNodeHandler(ctx, request)
		assert.NoError(t, err)
		assert.Equal(t, 403, response.StatusCode) // Forbidden
		assert.Contains(t, response.Body, "forbidden")
	})

	// Test case 4: Missing ACCOUNTS_TABLE environment variable
	t.Run("MissingAccountsTable", func(t *testing.T) {
		// Temporarily unset ACCOUNTS_TABLE
		originalValue := os.Getenv("ACCOUNTS_TABLE")
		_ = os.Unsetenv("ACCOUNTS_TABLE")
		defer func() { _ = os.Setenv("ACCOUNTS_TABLE", originalValue) }()

		testNode := createTestNode(testId + "_no_table")
		testNode.UserId = "different-user-" + testId // Different user to trigger account check
		
		err := nodeStore.Put(ctx, testNode)
		require.NoError(t, err)

		request := events.APIGatewayV2HTTPRequest{
			PathParameters: map[string]string{
				"id": testNode.Uuid,
			},
			RequestContext: events.APIGatewayV2HTTPRequestContext{
				Authorizer: test.CreateTestAuthorizer("some-other-user", testNode.OrganizationId),
			},
		}

		response, err := compute.DeleteComputeNodeHandler(ctx, request)
		assert.NoError(t, err)
		assert.Equal(t, 500, response.StatusCode)
		assert.Contains(t, response.Body, "error loading AWS config")
	})

	// Test case 5: Account not found
	t.Run("AccountNotFound", func(t *testing.T) {
		testNode := createTestNode(testId + "_no_account")
		testNode.UserId = "different-user-" + testId
		testNode.AccountUuid = "non-existent-account-" + uuid.New().String()
		
		err := nodeStore.Put(ctx, testNode)
		require.NoError(t, err)

		request := events.APIGatewayV2HTTPRequest{
			PathParameters: map[string]string{
				"id": testNode.Uuid,
			},
			RequestContext: events.APIGatewayV2HTTPRequestContext{
				Authorizer: test.CreateTestAuthorizer("some-other-user", testNode.OrganizationId),
			},
		}

		response, err := compute.DeleteComputeNodeHandler(ctx, request)
		assert.NoError(t, err)
		assert.Equal(t, 403, response.StatusCode) // Forbidden (account doesn't exist, so can't delete)
		assert.Contains(t, response.Body, "forbidden")
	})
}