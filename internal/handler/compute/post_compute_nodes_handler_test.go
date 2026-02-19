package compute_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/pennsieve/account-service/internal/handler/compute"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper functions for PostgreSQL integration testing
func setupTestPostgreSQL() (*sql.DB, error) {
	// Connect to the postgres database (the pennsieve schema is in the postgres database in the seeded image)
	db, err := sql.Open("postgres", "postgres://postgres:password@localhost:5432/postgres?sslmode=disable")
	if err != nil {
		return nil, err
	}

	// Create the schema if it doesn't exist
	_, err = db.Exec(`CREATE SCHEMA IF NOT EXISTS pennsieve`)
	if err != nil {
		db.Close()
		return nil, err
	}

	// Create the organization_user table if it doesn't exist
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS pennsieve.organization_user (
			user_id BIGINT,
			organization_id BIGINT,
			permission_bit INTEGER,
			PRIMARY KEY (user_id, organization_id)
		)
	`)
	if err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func setupOrganizationUser(db *sql.DB, userId, organizationId int64, permissionBit int) error {
	// Insert or update the user's organization membership
	query := `
		INSERT INTO pennsieve.organization_user (user_id, organization_id, permission_bit)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, organization_id)
		DO UPDATE SET permission_bit = $3
	`
	_, err := db.Exec(query, userId, organizationId, permissionBit)
	return err
}

func setupPostComputeNodesHandlerTest(t *testing.T) (store_dynamodb.DynamoDBStore, store_dynamodb.NodeAccessStore, store_dynamodb.AccountWorkspaceStore) {
	// Use shared test client
	client := test.GetClient()
	accountStore := store_dynamodb.NewAccountDatabaseStore(client, test.TEST_ACCOUNTS_WITH_INDEX_TABLE)
	nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(client, TEST_ACCESS_TABLE)
	workspaceStore := store_dynamodb.NewAccountWorkspaceStore(client, test.TEST_WORKSPACE_TABLE)
	
	// Set environment variables for handler
	os.Setenv("ACCOUNTS_TABLE", test.TEST_ACCOUNTS_WITH_INDEX_TABLE)
	os.Setenv("NODE_ACCESS_TABLE", TEST_ACCESS_TABLE)
	os.Setenv("ACCOUNT_WORKSPACE_TABLE", test.TEST_WORKSPACE_TABLE)
	os.Setenv("COMPUTE_NODES_TABLE", TEST_NODES_TABLE)
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
	
	return accountStore, nodeAccessStore, workspaceStore
}

func TestPostComputeNodesHandler_IndependentNodeSuccess(t *testing.T) {
	accountStore, _, _ := setupPostComputeNodesHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create test account
	testAccount := store_dynamodb.Account{
		Uuid:        uuid.New().String(),
		AccountId:   "test-account-" + testId,
		AccountType: "aws",
		UserId:      "user-" + testId,
	}
	err := accountStore.Insert(ctx, testAccount)
	require.NoError(t, err)

	// Create node request (organization-independent)
	nodeRequest := models.CreateComputeNodeRequest{
		Name:               "Test Node",
		Description:        "Test Description",
		AccountId:          testAccount.Uuid,
		OrganizationId:     "", // Empty for organization-independent
	}
	requestBody, err := json.Marshal(nodeRequest)
	require.NoError(t, err)

	// Create request with test authorizer
	request := events.APIGatewayV2HTTPRequest{
		Body: string(requestBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testAccount.UserId, ""),
		},
	}

	// Call the handler
	response, err := compute.PostComputeNodesHandler(ctx, request)
	assert.NoError(t, err)

	// Verify response (test environment returns created node)
	assert.Equal(t, 201, response.StatusCode) // Created

	// Parse response body to verify created node
	var createdNode models.Node
	err = json.Unmarshal([]byte(response.Body), &createdNode)
	assert.NoError(t, err)
	assert.Equal(t, "Test Node", createdNode.Name)
	assert.Equal(t, "Test Description", createdNode.Description)
	assert.Equal(t, "", createdNode.OrganizationId) // Should be empty (converted from INDEPENDENT)
	assert.Equal(t, testAccount.UserId, createdNode.OwnerId)
	assert.Equal(t, "Pending", createdNode.Status)
	assert.NotEmpty(t, createdNode.Uuid)
}

func TestPostComputeNodesHandler_BadRequest(t *testing.T) {
	_, _, _ = setupPostComputeNodesHandlerTest(t)
	ctx := context.Background()

	// Create request with invalid JSON body
	request := events.APIGatewayV2HTTPRequest{
		Body: "{invalid json",
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("test-user", "test-org"),
		},
	}

	// Call the handler
	response, err := compute.PostComputeNodesHandler(ctx, request)
	assert.NoError(t, err)

	// Should return 500 Internal Server Error (handler returns 500 for unmarshal errors)
	assert.Equal(t, 500, response.StatusCode)
	assert.Contains(t, response.Body, "error unmarshaling")
}

func TestPostComputeNodesHandler_MissingAccountUuid(t *testing.T) {
	_, _, _ = setupPostComputeNodesHandlerTest(t)
	ctx := context.Background()

	// Create node request without accountId
	nodeRequest := models.CreateComputeNodeRequest{
		Name:        "Test Node",
		Description: "Test Description",
		// AccountId missing
	}
	requestBody, err := json.Marshal(nodeRequest)
	require.NoError(t, err)

	// Create request with test authorizer
	request := events.APIGatewayV2HTTPRequest{
		Body: string(requestBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("test-user", ""),
		},
	}

	// Call the handler
	response, err := compute.PostComputeNodesHandler(ctx, request)
	assert.NoError(t, err)

	// Should return 400 Bad Request
	assert.Equal(t, 400, response.StatusCode)
	assert.Contains(t, response.Body, "bad request")
}

func TestPostComputeNodesHandler_AccountNotFound(t *testing.T) {
	_, _, _ = setupPostComputeNodesHandlerTest(t)
	ctx := context.Background()

	nonExistentAccountUuid := uuid.New().String()

	// Create node request with non-existent account
	nodeRequest := models.CreateComputeNodeRequest{
		Name:        "Test Node",
		Description: "Test Description",
		AccountId:   nonExistentAccountUuid,
	}
	requestBody, err := json.Marshal(nodeRequest)
	require.NoError(t, err)

	// Create request with test authorizer
	request := events.APIGatewayV2HTTPRequest{
		Body: string(requestBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("test-user", ""),
		},
	}

	// Call the handler
	response, err := compute.PostComputeNodesHandler(ctx, request)
	assert.NoError(t, err)

	// Should return 404 Not Found
	assert.Equal(t, 404, response.StatusCode)
	assert.Contains(t, response.Body, "not found")
}

func TestPostComputeNodesHandler_WorkspaceEnabledPrivateAccount(t *testing.T) {
	accountStore, _, workspaceStore := setupPostComputeNodesHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()
	orgId := "N:organization:123e4567-e89b-12d3-a456-426614174000"

	// Create test account
	testAccount := store_dynamodb.Account{
		Uuid:        uuid.New().String(),
		AccountId:   "test-account-" + testId,
		AccountType: "aws",
		UserId:      "account-owner-" + testId,
	}
	err := accountStore.Insert(ctx, testAccount)
	require.NoError(t, err)

	// Create workspace enablement (private - isPublic = false)
	workspaceEnablement := store_dynamodb.AccountWorkspace{
		AccountUuid: testAccount.Uuid,
		WorkspaceId: orgId,
		IsPublic:    false, // Private account
	}
	err = workspaceStore.Insert(ctx, workspaceEnablement)
	require.NoError(t, err)

	// Create node request for organization workspace
	nodeRequest := models.CreateComputeNodeRequest{
		Name:           "Test Workspace Node",
		Description:    "Test Description",
		AccountId:      testAccount.Uuid,
		OrganizationId: orgId,
	}
	requestBody, err := json.Marshal(nodeRequest)
	require.NoError(t, err)

	// Test case 1: Account owner can create nodes (private account)
	t.Run("AccountOwner_CanCreateNode", func(t *testing.T) {
		request := events.APIGatewayV2HTTPRequest{
			Body: string(requestBody),
			RequestContext: events.APIGatewayV2HTTPRequestContext{
				Authorizer: test.CreateTestAuthorizer(testAccount.UserId, orgId),
			},
		}

		response, err := compute.PostComputeNodesHandler(ctx, request)
		assert.NoError(t, err)
		assert.Equal(t, 201, response.StatusCode) // Created

		var createdNode models.Node
		err = json.Unmarshal([]byte(response.Body), &createdNode)
		assert.NoError(t, err)
		assert.Equal(t, orgId, createdNode.OrganizationId)
		assert.Equal(t, testAccount.UserId, createdNode.OwnerId)
	})

	// Test case 2: Non-account owner cannot create nodes (private account)
	t.Run("RandomUser_CannotCreateNode", func(t *testing.T) {
		randomUserId := "random-user-" + testId
		request := events.APIGatewayV2HTTPRequest{
			Body: string(requestBody),
			RequestContext: events.APIGatewayV2HTTPRequestContext{
				Authorizer: test.CreateTestAuthorizer(randomUserId, orgId),
			},
		}

		response, err := compute.PostComputeNodesHandler(ctx, request)
		assert.NoError(t, err)
		assert.Equal(t, 403, response.StatusCode) // Forbidden
		assert.Contains(t, response.Body, "only the account owner can create nodes on private accounts")
	})
}

func TestPostComputeNodesHandler_WorkspaceNotEnabled(t *testing.T) {
	accountStore, _, _ := setupPostComputeNodesHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()
	orgId := "N:organization:456e4567-e89b-12d3-a456-426614174001"

	// Create test account
	testAccount := store_dynamodb.Account{
		Uuid:        uuid.New().String(),
		AccountId:   "test-account-" + testId,
		AccountType: "aws",
		UserId:      "account-owner-" + testId,
	}
	err := accountStore.Insert(ctx, testAccount)
	require.NoError(t, err)

	// No workspace enablement record created

	// Create node request for organization workspace
	nodeRequest := models.CreateComputeNodeRequest{
		Name:           "Test Workspace Node",
		Description:    "Test Description",
		AccountId:      testAccount.Uuid,
		OrganizationId: orgId,
	}
	requestBody, err := json.Marshal(nodeRequest)
	require.NoError(t, err)

	request := events.APIGatewayV2HTTPRequest{
		Body: string(requestBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testAccount.UserId, orgId),
		},
	}

	// Call the handler
	response, err := compute.PostComputeNodesHandler(ctx, request)
	assert.NoError(t, err)

	// Should return 403 Forbidden (account not enabled for workspace)
	assert.Equal(t, 403, response.StatusCode)
	assert.Contains(t, response.Body, "account is not enabled for this workspace")
}

func TestPostComputeNodesHandler_MissingEnvironmentVariables(t *testing.T) {
	_, _, _ = setupPostComputeNodesHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create test account first (environment check happens after account validation)
	testAccount := store_dynamodb.Account{
		Uuid:        uuid.New().String(),
		AccountId:   "test-account-" + testId,
		AccountType: "aws",
		UserId:      "user-" + testId,
	}
	accountStore, _, _ := setupPostComputeNodesHandlerTest(t)
	err := accountStore.Insert(ctx, testAccount)
	require.NoError(t, err)

	// Create basic node request
	nodeRequest := models.CreateComputeNodeRequest{
		Name:           "Test Node",
		Description:    "Test Description",
		AccountId:      testAccount.Uuid,
		OrganizationId: "N:organization:123e4567-e89b-12d3-a456-426614174000",
	}
	requestBody, err := json.Marshal(nodeRequest)
	require.NoError(t, err)

	request := events.APIGatewayV2HTTPRequest{
		Body: string(requestBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("user-" + testId, "123"),
		},
	}

	// Test missing ACCOUNT_WORKSPACE_TABLE
	t.Run("MissingWorkspaceTable", func(t *testing.T) {
		originalValue := os.Getenv("ACCOUNT_WORKSPACE_TABLE")
		_ = os.Unsetenv("ACCOUNT_WORKSPACE_TABLE")
		defer func() { _ = os.Setenv("ACCOUNT_WORKSPACE_TABLE", originalValue) }()

		response, err := compute.PostComputeNodesHandler(ctx, request)
		assert.NoError(t, err)
		assert.Equal(t, 500, response.StatusCode)
		assert.Contains(t, response.Body, "error loading AWS config")
	})
}

func TestPostComputeNodesHandler_DifferentAccountTypes(t *testing.T) {
	accountStore, _, _ := setupPostComputeNodesHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Test creating nodes with different account types
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
			// Create account with specific type
			testAccount := store_dynamodb.Account{
				Uuid:        uuid.New().String(),
				AccountId:   "test-account-" + testId + "-" + tc.accountType,
				AccountType: tc.accountType,
				UserId:      "user-" + testId,
			}
			err := accountStore.Insert(ctx, testAccount)
			require.NoError(t, err)

			nodeRequest := models.CreateComputeNodeRequest{
				Name:           "Test " + tc.name,
				Description:    "Test Description",
				AccountId:      testAccount.Uuid,
				OrganizationId: "", // Independent node
			}
			requestBody, err := json.Marshal(nodeRequest)
			require.NoError(t, err)

			request := events.APIGatewayV2HTTPRequest{
				Body: string(requestBody),
				RequestContext: events.APIGatewayV2HTTPRequestContext{
					Authorizer: test.CreateTestAuthorizer(testAccount.UserId, ""),
				},
			}

			response, err := compute.PostComputeNodesHandler(ctx, request)
			assert.NoError(t, err)
			assert.Equal(t, 201, response.StatusCode)

			var createdNode models.Node
			err = json.Unmarshal([]byte(response.Body), &createdNode)
			assert.NoError(t, err)
			assert.Equal(t, tc.accountType, createdNode.Account.AccountType)
		})
	}
}

func TestPostComputeNodesHandler_NodeFieldValidation(t *testing.T) {
	accountStore, _, _ := setupPostComputeNodesHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create test account
	testAccount := store_dynamodb.Account{
		Uuid:        uuid.New().String(),
		AccountId:   "test-account-" + testId,
		AccountType: "aws",
		UserId:      "user-" + testId,
	}
	err := accountStore.Insert(ctx, testAccount)
	require.NoError(t, err)

	testCases := []struct {
		name        string
		nodeRequest models.CreateComputeNodeRequest
	}{
		{
			"EmptyName",
			models.CreateComputeNodeRequest{
				Name:        "", // Empty name
				Description: "Test Description",
				AccountId:   testAccount.Uuid,
			},
		},
		{
			"EmptyDescription",
			models.CreateComputeNodeRequest{
				Name:        "Test Node",
				Description: "", // Empty description
				AccountId:   testAccount.Uuid,
			},
		},
		{
			"LongName",
			models.CreateComputeNodeRequest{
				Name:        "Very-long-name-that-might-test-limits-of-node-name-field-validation-and-database-constraints",
				Description: "Test Description",
				AccountId:   testAccount.Uuid,
			},
		},
		{
			"SpecialCharacters",
			models.CreateComputeNodeRequest{
				Name:        "Test-Node_With@Special#Characters",
				Description: "Description with special characters: !@#$%^&*()",
				AccountId:   testAccount.Uuid,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			requestBody, err := json.Marshal(tc.nodeRequest)
			require.NoError(t, err)

			request := events.APIGatewayV2HTTPRequest{
				Body: string(requestBody),
				RequestContext: events.APIGatewayV2HTTPRequestContext{
					Authorizer: test.CreateTestAuthorizer(testAccount.UserId, ""),
				},
			}

			response, err := compute.PostComputeNodesHandler(ctx, request)
			assert.NoError(t, err)
			
			// All should succeed in creation (validation happens at ECS level)
			assert.Equal(t, 201, response.StatusCode)

			var createdNode models.Node
			err = json.Unmarshal([]byte(response.Body), &createdNode)
			assert.NoError(t, err)
			assert.Equal(t, tc.nodeRequest.Name, createdNode.Name)
			assert.Equal(t, tc.nodeRequest.Description, createdNode.Description)
		})
	}
}

func TestPostComputeNodesHandler_InitialPermissions(t *testing.T) {
	accountStore, _, _ := setupPostComputeNodesHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create test account
	testAccount := store_dynamodb.Account{
		Uuid:        uuid.New().String(),
		AccountId:   "test-account-" + testId,
		AccountType: "aws",
		UserId:      "user-" + testId,
	}
	err := accountStore.Insert(ctx, testAccount)
	require.NoError(t, err)

	// Create node request
	nodeRequest := models.CreateComputeNodeRequest{
		Name:           "Test Node for Permissions",
		Description:    "Test Description",
		AccountId:      testAccount.Uuid,
		OrganizationId: "",
	}
	requestBody, err := json.Marshal(nodeRequest)
	require.NoError(t, err)

	request := events.APIGatewayV2HTTPRequest{
		Body: string(requestBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testAccount.UserId, ""),
		},
	}

	// Call the handler
	response, err := compute.PostComputeNodesHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 201, response.StatusCode)

	// Parse created node to get UUID
	var createdNode models.Node
	err = json.Unmarshal([]byte(response.Body), &createdNode)
	assert.NoError(t, err)

	// Verify initial permissions were set (should be private by default)
	// Note: This test verifies that the permission setup code runs without error
	// The actual permission validation would require the node to be in the database
	assert.NotEmpty(t, createdNode.Uuid)
	assert.Equal(t, testAccount.UserId, createdNode.OwnerId)
}

func TestPostComputeNodesHandler_WorkspaceEnabledPublicAccount(t *testing.T) {
	accountStore, _, workspaceStore := setupPostComputeNodesHandlerTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()
	orgId := "N:organization:789e4567-e89b-12d3-a456-426614174002"

	// Create test account (not owned by the test user)
	testAccount := store_dynamodb.Account{
		Uuid:        uuid.New().String(),
		AccountId:   "test-account-" + testId,
		AccountType: "aws",
		UserId:      "account-owner-" + testId, // Different from test user
	}
	err := accountStore.Insert(ctx, testAccount)
	require.NoError(t, err)

	// Create workspace enablement (public - isPublic = true)
	workspaceEnablement := store_dynamodb.AccountWorkspace{
		AccountUuid: testAccount.Uuid,
		WorkspaceId: orgId,
		IsPublic:    true, // Public account - workspace admins can create nodes
	}
	err = workspaceStore.Insert(ctx, workspaceEnablement)
	require.NoError(t, err)

	// Create node request for organization workspace
	nodeRequest := models.CreateComputeNodeRequest{
		Name:           "Test Public Workspace Node",
		Description:    "Test Description",
		AccountId:      testAccount.Uuid,
		OrganizationId: orgId,
	}
	requestBody, err := json.Marshal(nodeRequest)
	require.NoError(t, err)

	// Test case 1: Account owner can still create nodes (public account)
	t.Run("AccountOwner_CanCreateNode", func(t *testing.T) {
		request := events.APIGatewayV2HTTPRequest{
			Body: string(requestBody),
			RequestContext: events.APIGatewayV2HTTPRequestContext{
				Authorizer: test.CreateTestAuthorizer(testAccount.UserId, orgId),
			},
		}

		response, err := compute.PostComputeNodesHandler(ctx, request)
		assert.NoError(t, err)
		
		// If not 201, log the response for debugging
		if response.StatusCode != 201 {
			t.Logf("Response status: %d, body: %s", response.StatusCode, response.Body)
		}
		assert.Equal(t, 201, response.StatusCode) // Created

		var createdNode models.Node
		err = json.Unmarshal([]byte(response.Body), &createdNode)
		assert.NoError(t, err)
		assert.Equal(t, orgId, createdNode.OrganizationId)
		assert.Equal(t, testAccount.UserId, createdNode.OwnerId)
	})

	// Test case 2: Workspace admin can create nodes (public account)
	// Note: This test will skip PostgreSQL checking when POSTGRES_URL is not set
	// In real environment, the PostgreSQL admin check would verify workspace admin status
	t.Run("WorkspaceAdmin_CanCreateNode", func(t *testing.T) {
		adminUserId := "admin-user-" + testId
		request := events.APIGatewayV2HTTPRequest{
			Body: string(requestBody),
			RequestContext: events.APIGatewayV2HTTPRequestContext{
				Authorizer: test.CreateTestAuthorizer(adminUserId, orgId),
			},
		}

		response, err := compute.PostComputeNodesHandler(ctx, request)
		assert.NoError(t, err)
		
		// When POSTGRES_HOST is not set (test environment), the PostgreSQL connection won't be available
		// and the handler should return an error since isPublic = true requires admin check
		if os.Getenv("POSTGRES_HOST") == "" {
			// Without PostgreSQL, the handler should return an error for public accounts
			assert.Equal(t, 500, response.StatusCode) // Internal server error
		} else {
			// With PostgreSQL available, the admin check would run and might fail depending on test data
			// In a full integration test environment, you'd set up the PostgreSQL data appropriately
			t.Skip("PostgreSQL admin checking requires integration test setup")
		}
	})

	// Test case 3: Test with actual PostgreSQL integration (admin and non-admin)
	t.Run("PostgreSQL_AdminCheck", func(t *testing.T) {
		// Only run if PostgreSQL container is available
		db, err := setupTestPostgreSQL()
		if err != nil {
			t.Skip("PostgreSQL not available, skipping integration test")
			return
		}
		defer db.Close()

		// Set POSTGRES_URL to use our test database (postgres database contains the pennsieve schema)
		originalPostgresURL := os.Getenv("POSTGRES_URL")
		os.Setenv("POSTGRES_URL", "postgres://postgres:password@localhost:5432/postgres?sslmode=disable")
		defer func() {
			if originalPostgresURL == "" {
				os.Unsetenv("POSTGRES_URL")
			} else {
				os.Setenv("POSTGRES_URL", originalPostgresURL)
			}
		}()

		// Test admin user (has permission_bit >= 16)
		adminUserId := int64(100)
		orgIdInt := int64(789)
		
		// First, ensure the organization exists (insert or update)
		// The seeded database should already have the organizations table with node_id column
		_, err = db.Exec(`
			INSERT INTO pennsieve.organizations (id, name, slug, encryption_key_id, node_id)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (id) DO UPDATE SET node_id = $5
		`, orgIdInt, "test-org-"+testId, "test-org-"+testId, "test-key", orgId)
		if err != nil {
			t.Logf("Warning: Could not create organization: %v", err)
		}
		
		// Create the admin user in the users table
		adminCognitoId := uuid.New().String()
		_, err = db.Exec(`
			INSERT INTO pennsieve.users (id, email, first_name, last_name, node_id, cognito_id)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (id) DO NOTHING
		`, adminUserId, "admin@test.com", "Admin", "User", "100", adminCognitoId)
		if err != nil {
			t.Logf("Warning: Could not create admin user: %v", err)
		}
		
		// Insert admin user into organization_user table
		err = setupOrganizationUser(db, adminUserId, orgIdInt, 16) // Admin permission
		if err != nil {
			t.Fatalf("Failed to set up admin user: %v", err)
		}

		adminRequest := events.APIGatewayV2HTTPRequest{
			Body: string(requestBody),
			RequestContext: events.APIGatewayV2HTTPRequestContext{
				Authorizer: test.CreateTestAuthorizer("100", orgId), // Use string version of adminUserId
			},
		}

		adminResponse, err := compute.PostComputeNodesHandler(ctx, adminRequest)
		assert.NoError(t, err)
		assert.Equal(t, 201, adminResponse.StatusCode) // Admin can create node

		// Test non-admin user (has permission_bit < 16)
		nonAdminUserId := int64(101)
		
		// Create the non-admin user in the users table
		nonAdminCognitoId := uuid.New().String()
		_, err = db.Exec(`
			INSERT INTO pennsieve.users (id, email, first_name, last_name, node_id, cognito_id)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (id) DO NOTHING
		`, nonAdminUserId, "user@test.com", "Test", "User", "101", nonAdminCognitoId)
		if err != nil {
			t.Logf("Warning: Could not create non-admin user: %v", err)
		}
		
		err = setupOrganizationUser(db, nonAdminUserId, orgIdInt, 8) // Collaborator permission
		if err != nil {
			t.Fatalf("Failed to set up non-admin user: %v", err)
		}

		nonAdminRequest := events.APIGatewayV2HTTPRequest{
			Body: string(requestBody),
			RequestContext: events.APIGatewayV2HTTPRequestContext{
				Authorizer: test.CreateTestAuthorizer("101", orgId), // Use string version of nonAdminUserId
			},
		}

		nonAdminResponse, err := compute.PostComputeNodesHandler(ctx, nonAdminRequest)
		assert.NoError(t, err)
		assert.Equal(t, 403, nonAdminResponse.StatusCode) // Non-admin cannot create node
		assert.Contains(t, nonAdminResponse.Body, "only workspace administrators can create nodes on public accounts")

		// Clean up test data
		_, _ = db.Exec("DELETE FROM pennsieve.organization_user WHERE user_id IN ($1, $2)", adminUserId, nonAdminUserId)
	})

	// Test case 4: Non-admin user when PostgreSQL is not available
	t.Run("NonAdmin_CannotCreateNode_NoPostgreSQL", func(t *testing.T) {
		nonAdminUserId := "non-admin-user-" + testId
		request := events.APIGatewayV2HTTPRequest{
			Body: string(requestBody),
			RequestContext: events.APIGatewayV2HTTPRequestContext{
				Authorizer: test.CreateTestAuthorizer(nonAdminUserId, orgId),
			},
		}

		response, err := compute.PostComputeNodesHandler(ctx, request)
		assert.NoError(t, err)
		
		// When POSTGRES_HOST is not set (normal test environment), the PostgreSQL connection
		// won't be available and the handler should return an error for public accounts
		// This tests that the public account logic requires PostgreSQL for admin checks
		if os.Getenv("POSTGRES_HOST") == "" {
			assert.Equal(t, 500, response.StatusCode) // Internal server error - no PostgreSQL
		}
		
		// Note: In a production environment with PostgreSQL, non-admin users would be 
		// rejected with 403 status and "only workspace admins can create nodes" message
	})
}