package compute

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getTestDynamoDBEndpoint() string {
	if endpoint := os.Getenv("DYNAMODB_URL"); endpoint != "" {
		return endpoint
	}
	return "http://localhost:8000"
}

func createTestTables(client *dynamodb.Client, testName string) error {
	// Create accounts table with microsecond precision and test name to avoid conflicts
	timestamp := time.Now().Format("20060102150405.000000")
	accountsTableName := "test-accounts-" + testName + "-" + timestamp
	_, err := client.CreateTable(context.TODO(), &dynamodb.CreateTableInput{
		AttributeDefinitions: []types.AttributeDefinition{
			{
				AttributeName: aws.String("uuid"),
				AttributeType: types.ScalarAttributeTypeS,
			},
		},
		KeySchema: []types.KeySchemaElement{
			{
				AttributeName: aws.String("uuid"),
				KeyType:       types.KeyTypeHash,
			},
		},
		TableName:   aws.String(accountsTableName),
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		return err
	}

	// Create compute nodes table
	computeNodesTableName := "test-compute-nodes-" + testName + "-" + timestamp
	_, err = client.CreateTable(context.TODO(), &dynamodb.CreateTableInput{
		AttributeDefinitions: []types.AttributeDefinition{
			{
				AttributeName: aws.String("uuid"),
				AttributeType: types.ScalarAttributeTypeS,
			},
		},
		KeySchema: []types.KeySchemaElement{
			{
				AttributeName: aws.String("uuid"),
				KeyType:       types.KeyTypeHash,
			},
		},
		TableName:   aws.String(computeNodesTableName),
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		return err
	}

	// Create workspace enablements table
	workspaceEnablementsTableName := "test-workspace-enablements-" + testName + "-" + timestamp
	_, err = client.CreateTable(context.TODO(), &dynamodb.CreateTableInput{
		AttributeDefinitions: []types.AttributeDefinition{
			{
				AttributeName: aws.String("accountUuid"),
				AttributeType: types.ScalarAttributeTypeS,
			},
			{
				AttributeName: aws.String("workspaceId"),
				AttributeType: types.ScalarAttributeTypeS,
			},
		},
		KeySchema: []types.KeySchemaElement{
			{
				AttributeName: aws.String("accountUuid"),
				KeyType:       types.KeyTypeHash,
			},
			{
				AttributeName: aws.String("workspaceId"),
				KeyType:       types.KeyTypeRange,
			},
		},
		TableName:   aws.String(workspaceEnablementsTableName),
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		return err
	}

	// Create node access table with GSIs
	nodeAccessTableName := "test-node-access-" + testName + "-" + timestamp
	_, err = client.CreateTable(context.TODO(), &dynamodb.CreateTableInput{
		AttributeDefinitions: []types.AttributeDefinition{
			{
				AttributeName: aws.String("entityId"),
				AttributeType: types.ScalarAttributeTypeS,
			},
			{
				AttributeName: aws.String("nodeId"),
				AttributeType: types.ScalarAttributeTypeS,
			},
			{
				AttributeName: aws.String("organizationId"),
				AttributeType: types.ScalarAttributeTypeS,
			},
		},
		KeySchema: []types.KeySchemaElement{
			{
				AttributeName: aws.String("entityId"),
				KeyType:       types.KeyTypeHash,
			},
			{
				AttributeName: aws.String("nodeId"),
				KeyType:       types.KeyTypeRange,
			},
		},
		GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
			{
				IndexName: aws.String("nodeId-entityId-index"),
				KeySchema: []types.KeySchemaElement{
					{
						AttributeName: aws.String("nodeId"),
						KeyType:       types.KeyTypeHash,
					},
					{
						AttributeName: aws.String("entityId"),
						KeyType:       types.KeyTypeRange,
					},
				},
				Projection: &types.Projection{
					ProjectionType: types.ProjectionTypeAll,
				},
			},
			{
				IndexName: aws.String("organizationId-nodeId-index"),
				KeySchema: []types.KeySchemaElement{
					{
						AttributeName: aws.String("organizationId"),
						KeyType:       types.KeyTypeHash,
					},
					{
						AttributeName: aws.String("nodeId"),
						KeyType:       types.KeyTypeRange,
					},
				},
				Projection: &types.Projection{
					ProjectionType: types.ProjectionTypeAll,
				},
			},
		},
		TableName:   aws.String(nodeAccessTableName),
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		return err
	}

	// Set environment variables
	os.Setenv("ACCOUNTS_TABLE", accountsTableName)
	os.Setenv("COMPUTE_NODES_TABLE", computeNodesTableName)
	os.Setenv("ACCOUNT_WORKSPACE_ENABLEMENT_TABLE", workspaceEnablementsTableName)
	os.Setenv("NODE_ACCESS_TABLE", nodeAccessTableName)

	// Wait for tables to be ready
	waiter := dynamodb.NewTableExistsWaiter(client)
	waiter.Wait(context.TODO(), &dynamodb.DescribeTableInput{TableName: aws.String(accountsTableName)}, 30*time.Second)
	waiter.Wait(context.TODO(), &dynamodb.DescribeTableInput{TableName: aws.String(computeNodesTableName)}, 30*time.Second)
	waiter.Wait(context.TODO(), &dynamodb.DescribeTableInput{TableName: aws.String(workspaceEnablementsTableName)}, 30*time.Second)
	waiter.Wait(context.TODO(), &dynamodb.DescribeTableInput{TableName: aws.String(nodeAccessTableName)}, 30*time.Second)

	return nil
}

func setupTestClient() (*dynamodb.Client, error) {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     "test",
				SecretAccessKey: "test",
			}, nil
		})),
		config.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc(
			func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{URL: getTestDynamoDBEndpoint()}, nil
			})))
	if err != nil {
		return nil, err
	}

	return dynamodb.NewFromConfig(cfg), nil
}

func createTestNodeRequest(accountUuid, organizationId string) models.Node {
	return models.Node{
		Name:        "Test Node",
		Description: "Test Description",
		Account: models.NodeAccount{
			Uuid:        accountUuid,
			AccountId:   "123456789",
			AccountType: "aws",
		},
		OrganizationId: organizationId,
	}
}

func createTestAPIRequest(method, userId, organizationId string, node models.Node) events.APIGatewayV2HTTPRequest {
	nodeJSON, _ := json.Marshal(node)
	
	request := events.APIGatewayV2HTTPRequest{
		RouteKey: method + " /compute-nodes",
		Body:     string(nodeJSON),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
				Method: method,
			},
			Authorizer: &events.APIGatewayV2HTTPRequestContextAuthorizerDescription{
				Lambda: map[string]interface{}{
					"user_claim": map[string]interface{}{
						"Id":           float64(123),
						"NodeId":       userId,
						"IsSuperAdmin": false,
					},
					"org_claim": map[string]interface{}{
						"NodeId": organizationId,
						"IntId":  float64(456),
						"Role":   float64(16), // Admin role as numeric value
					},
					"iat": float64(1640995200),
					"exp": float64(1640998800),
				},
			},
		},
	}

	if organizationId != "" {
		request.QueryStringParameters = map[string]string{
			"organization_id": organizationId,
		}
		request.RequestContext.Authorizer.Lambda["organization_node_id"] = organizationId
	}

	return request
}

func TestPostComputeNodesHandler_MissingAccountUuid(t *testing.T) {
	// Create node without account UUID
	node := models.Node{
		Name:        "Test Node",
		Description: "Test Description",
		// Account is missing
	}
	
	request := createTestAPIRequest("POST", "user-123", "org-456", node)
	
	response, err := PostComputeNodesHandler(context.Background(), request)
	
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.Contains(t, response.Body, "bad request")
}

func TestPostComputeNodesHandler_OrganizationIndependentNode_Success(t *testing.T) {
	client, err := setupTestClient()
	require.NoError(t, err)

	err = createTestTables(client, "OrganizationIndependentNode_Success")
	require.NoError(t, err)

	// Set environment variables
	os.Setenv("ENV", "TEST")

	defer func() {
		os.Unsetenv("ENV")
		os.Unsetenv("ACCOUNTS_TABLE")
		os.Unsetenv("COMPUTE_NODES_TABLE")
		os.Unsetenv("ACCOUNT_WORKSPACE_ENABLEMENT_TABLE")
		os.Unsetenv("NODE_ACCESS_TABLE")
	}()

	// Create test account
	accountStore := store_dynamodb.NewAccountDatabaseStore(client, os.Getenv("ACCOUNTS_TABLE"))
	testAccount := store_dynamodb.Account{
		Uuid:        "account-123",
		AccountId:   "123456789",
		AccountType: "aws",
		UserId:      "user-123",
		RoleName:    "test-role",
		ExternalId:  "ext-123",
		Name:        "Test Account",
		Status:      "Enabled",
	}
	err = accountStore.Insert(context.Background(), testAccount)
	require.NoError(t, err)

	// Create organization-independent node (no organizationId)
	node := createTestNodeRequest("account-123", "") // Empty organizationId
	request := createTestAPIRequest("POST", "user-123", "", node)
	
	response, err := PostComputeNodesHandler(context.Background(), request)
	
	assert.NoError(t, err)
	assert.Equal(t, http.StatusCreated, response.StatusCode)
	
	var responseNode models.Node
	err = json.Unmarshal([]byte(response.Body), &responseNode)
	assert.NoError(t, err)
	assert.Equal(t, "Test Node", responseNode.Name)
	assert.Equal(t, "", responseNode.OrganizationId) // Organization-independent
	assert.Equal(t, "user-123", responseNode.UserId)
}

func TestPostComputeNodesHandler_PrivateAccount_OnlyOwnerCanCreate(t *testing.T) {
	client, err := setupTestClient()
	require.NoError(t, err)

	err = createTestTables(client, "PrivateAccount_OnlyOwnerCanCreate")
	require.NoError(t, err)

	// Set environment variables
	os.Setenv("ENV", "TEST")

	defer func() {
		os.Unsetenv("ENV")
		os.Unsetenv("ACCOUNTS_TABLE")
		os.Unsetenv("COMPUTE_NODES_TABLE")
		os.Unsetenv("ACCOUNT_WORKSPACE_ENABLEMENT_TABLE")
	}()

	// Create test account in DynamoDB (owner is different from requester)
	accountStore := store_dynamodb.NewAccountDatabaseStore(client, os.Getenv("ACCOUNTS_TABLE"))
	testAccount := store_dynamodb.Account{
		Uuid:        "account-123",
		AccountId:   "123456789",
		AccountType: "aws",
		UserId:      "owner-456", // Different from requester
		RoleName:    "test-role",
		ExternalId:  "ext-123",
		Name:        "Test Account",
		Status:      "Enabled",
	}
	err = accountStore.Insert(context.Background(), testAccount)
	require.NoError(t, err)

	// Create workspace enablement (private account)
	workspaceStore := store_dynamodb.NewAccountWorkspaceStore(client, os.Getenv("ACCOUNT_WORKSPACE_ENABLEMENT_TABLE"))
	enablement := store_dynamodb.AccountWorkspace{
		AccountUuid: "account-123",
		WorkspaceId: "org-456",
		IsPublic:    false, // Private account
		EnabledBy:   "user-123",
		EnabledAt:   time.Now().Unix(),
	}
	err = workspaceStore.Insert(context.Background(), enablement)
	require.NoError(t, err)

	// Try to create node as non-owner
	node := createTestNodeRequest("account-123", "org-456")
	request := createTestAPIRequest("POST", "non-owner-789", "org-456", node)
	
	response, err := PostComputeNodesHandler(context.Background(), request)
	
	assert.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, response.StatusCode)
	assert.Contains(t, response.Body, "only the account owner can create nodes")
}

func TestPostComputeNodesHandler_PrivateAccount_OwnerCanCreate(t *testing.T) {
	client, err := setupTestClient()
	require.NoError(t, err)

	err = createTestTables(client, "PrivateAccount_OwnerCanCreate")
	require.NoError(t, err)

	// Set environment variables
	os.Setenv("ENV", "TEST")

	defer func() {
		os.Unsetenv("ENV")
		os.Unsetenv("ACCOUNTS_TABLE")
		os.Unsetenv("COMPUTE_NODES_TABLE")
		os.Unsetenv("ACCOUNT_WORKSPACE_ENABLEMENT_TABLE")
	}()

	// Create test account in DynamoDB
	accountStore := store_dynamodb.NewAccountDatabaseStore(client, os.Getenv("ACCOUNTS_TABLE"))
	testAccount := store_dynamodb.Account{
		Uuid:        "account-123",
		AccountId:   "123456789",
		AccountType: "aws",
		UserId:      "user-123", // Same as requester
		RoleName:    "test-role",
		ExternalId:  "ext-123",
		Name:        "Test Account",
		Status:      "Enabled",
	}
	err = accountStore.Insert(context.Background(), testAccount)
	require.NoError(t, err)

	// Create workspace enablement (private account)
	workspaceStore := store_dynamodb.NewAccountWorkspaceStore(client, os.Getenv("ACCOUNT_WORKSPACE_ENABLEMENT_TABLE"))
	enablement := store_dynamodb.AccountWorkspace{
		AccountUuid: "account-123",
		WorkspaceId: "org-456",
		IsPublic:    false, // Private account
		EnabledBy:   "user-123",
		EnabledAt:   time.Now().Unix(),
	}
	err = workspaceStore.Insert(context.Background(), enablement)
	require.NoError(t, err)

	// Create node as owner
	node := createTestNodeRequest("account-123", "org-456")
	request := createTestAPIRequest("POST", "user-123", "org-456", node)
	
	response, err := PostComputeNodesHandler(context.Background(), request)
	
	assert.NoError(t, err)
	assert.Equal(t, http.StatusCreated, response.StatusCode)

	var responseNode models.Node
	err = json.Unmarshal([]byte(response.Body), &responseNode)
	assert.NoError(t, err)
	assert.Equal(t, "Test Node", responseNode.Name)
	assert.Equal(t, "org-456", responseNode.OrganizationId)
	assert.Equal(t, "user-123", responseNode.UserId)
}

func TestPostComputeNodesHandler_AccountNotEnabledForWorkspace(t *testing.T) {
	client, err := setupTestClient()
	require.NoError(t, err)

	err = createTestTables(client, "AccountNotEnabledForWorkspace")
	require.NoError(t, err)

	// Set environment variables
	os.Setenv("ENV", "TEST")

	defer func() {
		os.Unsetenv("ENV")
		os.Unsetenv("ACCOUNTS_TABLE")
		os.Unsetenv("COMPUTE_NODES_TABLE")
		os.Unsetenv("ACCOUNT_WORKSPACE_ENABLEMENT_TABLE")
	}()

	// Create test account in DynamoDB but no workspace enablement
	accountStore := store_dynamodb.NewAccountDatabaseStore(client, os.Getenv("ACCOUNTS_TABLE"))
	testAccount := store_dynamodb.Account{
		Uuid:        "account-123",
		AccountId:   "123456789",
		AccountType: "aws",
		UserId:      "user-123",
		RoleName:    "test-role",
		ExternalId:  "ext-123",
		Name:        "Test Account",
		Status:      "Enabled",
	}
	err = accountStore.Insert(context.Background(), testAccount)
	require.NoError(t, err)

	// Try to create node for workspace that account is not enabled for
	node := createTestNodeRequest("account-123", "org-456")
	request := createTestAPIRequest("POST", "user-123", "org-456", node)
	
	response, err := PostComputeNodesHandler(context.Background(), request)
	
	assert.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, response.StatusCode)
	assert.Contains(t, response.Body, "account is not enabled for this workspace")
}