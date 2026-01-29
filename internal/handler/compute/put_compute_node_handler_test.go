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

func setupPutComputeNodeHandlerTest(t *testing.T) (store_dynamodb.NodeStore, store_dynamodb.DynamoDBStore) {
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
        os.Setenv("ENV", "TEST") // This triggers test-aware config in LoadAWSConfig
    }
    // Don't override DYNAMODB_URL if already set (Docker sets it to http://dynamodb:8000)
    if os.Getenv("DYNAMODB_URL") == "" {
        os.Setenv("DYNAMODB_URL", "http://localhost:8000") // Local DynamoDB endpoint for local testing
    }

    return nodeStore, accountStore
}

func TestPutComputeNodeHandler_Success(t *testing.T) {
    nodeStore, _ := setupPutComputeNodeHandlerTest(t)
    ctx := context.Background()
    testId := test.GenerateTestId()

    // Create and insert test node
    testNode := createTestNode(testId)
    err := nodeStore.Put(ctx, testNode)
    require.NoError(t, err)

    // Create update request
    updateRequest := models.NodeUpdateRequest{
        WorkflowManagerTag:    "updated-tag",
        WorkflowManagerCpu:    4096,
        WorkflowManagerMemory: 8192,
        AuthorizationType:     "AWS_IAM",
    }
    requestBody, err := json.Marshal(updateRequest)
    require.NoError(t, err)

    // Create request with test authorizer
    request := events.APIGatewayV2HTTPRequest{
        PathParameters: map[string]string{
            "id": testNode.Uuid,
        },
        Body: string(requestBody),
        RequestContext: events.APIGatewayV2HTTPRequestContext{
            Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
        },
    }

    // Call the handler
    response, err := compute.PutComputeNodeHandler(ctx, request)
    assert.NoError(t, err)

    // Verify response
    assert.Equal(t, 202, response.StatusCode) // Accepted (async operation)

    // Parse response body
    var nodeResponse models.NodeResponse
    err = json.Unmarshal([]byte(response.Body), &nodeResponse)
    assert.NoError(t, err)
    assert.Equal(t, "Compute node update initiated", nodeResponse.Message)
}

func TestPutComputeNodeHandler_NotFound(t *testing.T) {
    _, _ = setupPutComputeNodeHandlerTest(t)
    ctx := context.Background()

    nonExistentId := uuid.New().String()

    // Create update request
    updateRequest := models.NodeUpdateRequest{
        WorkflowManagerTag: "test-tag",
    }
    requestBody, err := json.Marshal(updateRequest)
    require.NoError(t, err)

    // Create request for non-existent node with test authorizer
    request := events.APIGatewayV2HTTPRequest{
        PathParameters: map[string]string{
            "id": nonExistentId,
        },
        Body: string(requestBody),
        RequestContext: events.APIGatewayV2HTTPRequestContext{
            Authorizer: test.CreateTestAuthorizer("test-user", "test-org"),
        },
    }

    // Call the handler
    response, err := compute.PutComputeNodeHandler(ctx, request)
    assert.NoError(t, err)

    // Should return 404 Not Found
    assert.Equal(t, 404, response.StatusCode)
    assert.Contains(t, response.Body, "no records found")
}

func TestPutComputeNodeHandler_BadRequest(t *testing.T) {
    _, _ = setupPutComputeNodeHandlerTest(t)
    ctx := context.Background()

    // Create request with invalid JSON body
    request := events.APIGatewayV2HTTPRequest{
        PathParameters: map[string]string{
            "id": "test-id",
        },
        Body: "{invalid json",
        RequestContext: events.APIGatewayV2HTTPRequestContext{
            Authorizer: test.CreateTestAuthorizer("test-user", "test-org"),
        },
    }

    // Call the handler
    response, err := compute.PutComputeNodeHandler(ctx, request)
    assert.NoError(t, err)

    // Should return 400 Bad Request
    assert.Equal(t, 400, response.StatusCode)
    assert.Contains(t, response.Body, "error unmarshaling")
}

func TestPutComputeNodeHandler_MissingId(t *testing.T) {
    _, _ = setupPutComputeNodeHandlerTest(t)
    ctx := context.Background()

    // Create update request
    updateRequest := models.NodeUpdateRequest{
        WorkflowManagerTag: "test-tag",
    }
    requestBody, err := json.Marshal(updateRequest)
    require.NoError(t, err)

    // Create request without ID parameter but with test authorizer
    request := events.APIGatewayV2HTTPRequest{
        PathParameters: map[string]string{},
        Body:           string(requestBody),
        RequestContext: events.APIGatewayV2HTTPRequestContext{
            Authorizer: test.CreateTestAuthorizer("test-user", "test-org"),
        },
    }

    // Call the handler
    response, err := compute.PutComputeNodeHandler(ctx, request)
    assert.NoError(t, err)

    // When ID is missing, DynamoDB returns an error for empty string key
    // This should result in a 500 Internal Server Error
    assert.Equal(t, 500, response.StatusCode)
    assert.Contains(t, response.Body, "error performing action on DynamoDB table")
}

func TestPutComputeNodeHandler_DefaultValues(t *testing.T) {
    nodeStore, _ := setupPutComputeNodeHandlerTest(t)
    ctx := context.Background()
    testId := test.GenerateTestId()

    // Create and insert test node
    testNode := createTestNode(testId)
    err := nodeStore.Put(ctx, testNode)
    require.NoError(t, err)

    // Create update request with empty/zero values to test defaults
    updateRequest := models.NodeUpdateRequest{
        // All fields left empty/zero to test defaults
    }
    requestBody, err := json.Marshal(updateRequest)
    require.NoError(t, err)

    // Create request with test authorizer
    request := events.APIGatewayV2HTTPRequest{
        PathParameters: map[string]string{
            "id": testNode.Uuid,
        },
        Body: string(requestBody),
        RequestContext: events.APIGatewayV2HTTPRequestContext{
            Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
        },
    }

    // Call the handler
    response, err := compute.PutComputeNodeHandler(ctx, request)
    assert.NoError(t, err)

    // Should successfully use defaults
    assert.Equal(t, 202, response.StatusCode)
    assert.Contains(t, response.Body, "Compute node update initiated")
}

func TestPutComputeNodeHandler_PartialUpdate(t *testing.T) {
    nodeStore, _ := setupPutComputeNodeHandlerTest(t)
    ctx := context.Background()
    testId := test.GenerateTestId()

    // Create and insert test node
    testNode := createTestNode(testId)
    err := nodeStore.Put(ctx, testNode)
    require.NoError(t, err)

    testCases := []struct {
        name    string
        request models.NodeUpdateRequest
    }{
        {
            "Update Only Tag",
            models.NodeUpdateRequest{
                WorkflowManagerTag: "new-tag",
            },
        },
        {
            "Update Only CPU",
            models.NodeUpdateRequest{
                WorkflowManagerCpu: 1024,
            },
        },
        {
            "Update Only Memory",
            models.NodeUpdateRequest{
                WorkflowManagerMemory: 2048,
            },
        },
        {
            "Update Only Auth Type",
            models.NodeUpdateRequest{
                AuthorizationType: "AWS_IAM",
            },
        },
    }

    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            requestBody, err := json.Marshal(tc.request)
            require.NoError(t, err)

            request := events.APIGatewayV2HTTPRequest{
                PathParameters: map[string]string{
                    "id": testNode.Uuid,
                },
                Body: string(requestBody),
                RequestContext: events.APIGatewayV2HTTPRequestContext{
                    Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
                },
            }

            response, err := compute.PutComputeNodeHandler(ctx, request)
            assert.NoError(t, err)
            assert.Equal(t, 202, response.StatusCode)
            assert.Contains(t, response.Body, "Compute node update initiated")
        })
    }
}

func TestPutComputeNodeHandler_PermissionChecks(t *testing.T) {
    nodeStore, accountStore := setupPutComputeNodeHandlerTest(t)
    ctx := context.Background()
    testId := test.GenerateTestId()

    // Create update request
    updateRequest := models.NodeUpdateRequest{
        WorkflowManagerTag: "updated-tag",
    }
    requestBody, err := json.Marshal(updateRequest)
    require.NoError(t, err)

    // Test case 1: Node owner can update
    t.Run("NodeOwner_CanUpdate", func(t *testing.T) {
        testNode := createTestNode(testId + "_owner")
        nodeOwner := testNode.UserId

        err := nodeStore.Put(ctx, testNode)
        require.NoError(t, err)

        request := events.APIGatewayV2HTTPRequest{
            PathParameters: map[string]string{
                "id": testNode.Uuid,
            },
            Body: string(requestBody),
            RequestContext: events.APIGatewayV2HTTPRequestContext{
                Authorizer: test.CreateTestAuthorizer(nodeOwner, testNode.OrganizationId),
            },
        }

        response, err := compute.PutComputeNodeHandler(ctx, request)
        assert.NoError(t, err)
        assert.Equal(t, 202, response.StatusCode)
        assert.Contains(t, response.Body, "Compute node update initiated")
    })

    // Test case 2: Account owner can update
    t.Run("AccountOwner_CanUpdate", func(t *testing.T) {
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
            Body: string(requestBody),
            RequestContext: events.APIGatewayV2HTTPRequestContext{
                Authorizer: test.CreateTestAuthorizer(testAccount.UserId, testNode.OrganizationId),
            },
        }

        response, err := compute.PutComputeNodeHandler(ctx, request)
        assert.NoError(t, err)
        assert.Equal(t, 202, response.StatusCode)
        assert.Contains(t, response.Body, "Compute node update initiated")
    })

    // Test case 3: Non-owner, non-account-owner cannot update
    t.Run("RandomUser_CannotUpdate", func(t *testing.T) {
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
            Body: string(requestBody),
            RequestContext: events.APIGatewayV2HTTPRequestContext{
                Authorizer: test.CreateTestAuthorizer(randomUserId, testNode.OrganizationId),
            },
        }

        response, err := compute.PutComputeNodeHandler(ctx, request)
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
            Body: string(requestBody),
            RequestContext: events.APIGatewayV2HTTPRequestContext{
                Authorizer: test.CreateTestAuthorizer("some-other-user", testNode.OrganizationId),
            },
        }

        response, err := compute.PutComputeNodeHandler(ctx, request)
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
            Body: string(requestBody),
            RequestContext: events.APIGatewayV2HTTPRequestContext{
                Authorizer: test.CreateTestAuthorizer("some-other-user", testNode.OrganizationId),
            },
        }

        response, err := compute.PutComputeNodeHandler(ctx, request)
        assert.NoError(t, err)
        assert.Equal(t, 403, response.StatusCode) // Forbidden (account doesn't exist, so can't update)
        assert.Contains(t, response.Body, "forbidden")
    })
}

func TestPutComputeNodeHandler_OrganizationIndependentNode(t *testing.T) {
    nodeStore, _ := setupPutComputeNodeHandlerTest(t)
    ctx := context.Background()
    testId := test.GenerateTestId()

    // Create organization-independent node
    testNode := createTestNode(testId)
    testNode.OrganizationId = "INDEPENDENT" // Organization-independent node

    err := nodeStore.Put(ctx, testNode)
    require.NoError(t, err)

    // Create update request
    updateRequest := models.NodeUpdateRequest{
        WorkflowManagerTag: "updated-tag",
    }
    requestBody, err := json.Marshal(updateRequest)
    require.NoError(t, err)

    // Create request with test authorizer (no organization claim)
    request := events.APIGatewayV2HTTPRequest{
        PathParameters: map[string]string{
            "id": testNode.Uuid,
        },
        Body: string(requestBody),
        RequestContext: events.APIGatewayV2HTTPRequestContext{
            Authorizer: test.CreateTestAuthorizer(testNode.UserId, ""), // No org claim
        },
    }

    // Call the handler
    response, err := compute.PutComputeNodeHandler(ctx, request)
    assert.NoError(t, err)

    // Should successfully initiate update
    assert.Equal(t, 202, response.StatusCode)

    // Parse response body
    var nodeResponse models.NodeResponse
    err = json.Unmarshal([]byte(response.Body), &nodeResponse)
    assert.NoError(t, err)
    assert.Equal(t, "Compute node update initiated", nodeResponse.Message)
}

func TestPutComputeNodeHandler_DifferentAccountTypes(t *testing.T) {
    nodeStore, _ := setupPutComputeNodeHandlerTest(t)
    ctx := context.Background()
    testId := test.GenerateTestId()

    // Create update request
    updateRequest := models.NodeUpdateRequest{
        WorkflowManagerTag: "updated-tag",
    }
    requestBody, err := json.Marshal(updateRequest)
    require.NoError(t, err)

    // Test updating nodes with different account types
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

            // Request to update the node
            request := events.APIGatewayV2HTTPRequest{
                PathParameters: map[string]string{
                    "id": testNode.Uuid,
                },
                Body: string(requestBody),
                RequestContext: events.APIGatewayV2HTTPRequestContext{
                    Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
                },
            }

            response, err := compute.PutComputeNodeHandler(ctx, request)
            assert.NoError(t, err)

            // Should successfully initiate update regardless of account type
            assert.Equal(t, 202, response.StatusCode)

            var nodeResponse models.NodeResponse
            err = json.Unmarshal([]byte(response.Body), &nodeResponse)
            assert.NoError(t, err)
            assert.Equal(t, "Compute node update initiated", nodeResponse.Message)
        })
    }
}

func TestPutComputeNodeHandler_EdgeCaseValues(t *testing.T) {
    nodeStore, _ := setupPutComputeNodeHandlerTest(t)
    ctx := context.Background()
    testId := test.GenerateTestId()

    // Create and insert test node
    testNode := createTestNode(testId)
    err := nodeStore.Put(ctx, testNode)
    require.NoError(t, err)

    testCases := []struct {
        name    string
        request models.NodeUpdateRequest
    }{
        {
            "Very Large CPU",
            models.NodeUpdateRequest{
                WorkflowManagerCpu: 32768,
            },
        },
        {
            "Very Large Memory",
            models.NodeUpdateRequest{
                WorkflowManagerMemory: 65536,
            },
        },
        {
            "Empty Auth Type",
            models.NodeUpdateRequest{
                AuthorizationType: "",
            },
        },
        {
            "Long Tag Name",
            models.NodeUpdateRequest{
                WorkflowManagerTag: "very-long-tag-name-that-might-test-limits-of-container-environment-variables",
            },
        },
    }

    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            requestBody, err := json.Marshal(tc.request)
            require.NoError(t, err)

            request := events.APIGatewayV2HTTPRequest{
                PathParameters: map[string]string{
                    "id": testNode.Uuid,
                },
                Body: string(requestBody),
                RequestContext: events.APIGatewayV2HTTPRequestContext{
                    Authorizer: test.CreateTestAuthorizer(testNode.UserId, testNode.OrganizationId),
                },
            }

            response, err := compute.PutComputeNodeHandler(ctx, request)
            assert.NoError(t, err)
            assert.Equal(t, 202, response.StatusCode)
            assert.Contains(t, response.Body, "Compute node update initiated")
        })
    }
}
