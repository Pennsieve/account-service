package storage_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/google/uuid"
	"github.com/pennsieve/account-service/internal/handler/storage"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	if err := test.SetupPackageTables(); err != nil {
		panic(err)
	}
	code := m.Run()
	os.Exit(code)
}

func setupStorageHandlerTest(t *testing.T) (store_dynamodb.DynamoDBStore, store_dynamodb.StorageNodeStore, store_dynamodb.StorageNodeWorkspaceStore) {
	client := test.GetClient()
	accountStore := store_dynamodb.NewAccountDatabaseStore(client, test.TEST_ACCOUNTS_WITH_INDEX_TABLE)
	storageNodeStore := store_dynamodb.NewStorageNodeDatabaseStore(client, test.TEST_STORAGE_NODES_TABLE)
	storageNodeWorkspaceStore := store_dynamodb.NewStorageNodeWorkspaceStore(client, test.TEST_STORAGE_NODE_WORKSPACE_TABLE)

	os.Setenv("ACCOUNTS_TABLE", test.TEST_ACCOUNTS_WITH_INDEX_TABLE)
	os.Setenv("ACCOUNT_WORKSPACE_TABLE", test.TEST_WORKSPACE_TABLE)
	os.Setenv("NODE_ACCESS_TABLE", "test-access-table")
	os.Setenv("STORAGE_NODES_TABLE", test.TEST_STORAGE_NODES_TABLE)
	os.Setenv("STORAGE_NODE_WORKSPACE_TABLE", test.TEST_STORAGE_NODE_WORKSPACE_TABLE)
	if os.Getenv("ENV") == "" {
		os.Setenv("ENV", "TEST")
	}
	if os.Getenv("DYNAMODB_URL") == "" {
		os.Setenv("DYNAMODB_URL", "http://localhost:8000")
	}

	return accountStore, storageNodeStore, storageNodeWorkspaceStore
}

const testOrgId = "N:organization:00000000-0000-0000-0000-000000000001"

func createTestWorkspaceEnablement(t *testing.T, accountUuid, userId string) {
	client := test.GetClient()
	wsStore := store_dynamodb.NewAccountWorkspaceStore(client, test.TEST_WORKSPACE_TABLE)
	enablement := store_dynamodb.AccountWorkspace{
		AccountUuid:   accountUuid,
		WorkspaceId:   testOrgId,
		IsPublic:      true,
		EnableCompute: true,
		EnableStorage: true,
		EnabledBy:     userId,
		EnabledAt:     1000000000,
	}
	err := wsStore.Insert(context.Background(), enablement)
	require.NoError(t, err)
}

func createTestAccount(t *testing.T, accountStore store_dynamodb.DynamoDBStore, userId string) store_dynamodb.Account {
	testAccount := store_dynamodb.Account{
		Uuid:        uuid.New().String(),
		AccountId:   "test-account-" + uuid.New().String()[:8],
		AccountType: "aws",
		UserId:      userId,
		Status:      "Enabled",
	}
	err := accountStore.Insert(context.Background(), testAccount)
	require.NoError(t, err)
	return testAccount
}

// --- POST /storage-nodes ---

func TestPostStorageNodeHandler_Success(t *testing.T) {
	accountStore, _, wsStore := setupStorageHandlerTest(t)
	ctx := context.Background()
	userId := "user-" + test.GenerateTestId()
	testAccount := createTestAccount(t, accountStore, userId)
	createTestWorkspaceEnablement(t, testAccount.Uuid, userId)

	reqBody, _ := json.Marshal(models.CreateStorageNodeRequest{
		AccountUuid:      testAccount.Uuid,
		OrganizationId:   testOrgId,
		Name:             "SPARC Storage",
		Description:      "SPARC primary storage bucket",
		StorageLocation:  "pennsieve-sparc-storage",
		Region:           "us-east-1",
		ProviderType:     "s3",
		SkipProvisioning: true,
	})

	request := events.APIGatewayV2HTTPRequest{
		Body: string(reqBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, testOrgId),
		},
	}

	response, err := storage.PostStorageNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 201, response.StatusCode)

	var node models.StorageNode
	err = json.Unmarshal([]byte(response.Body), &node)
	assert.NoError(t, err)
	assert.NotEmpty(t, node.Uuid)
	assert.Equal(t, "SPARC Storage", node.Name)
	assert.Equal(t, "pennsieve-sparc-storage", node.StorageLocation)
	assert.Equal(t, "s3", node.ProviderType)
	assert.Equal(t, "us-east-1", node.Region)
	assert.Equal(t, "Enabled", node.Status)
	assert.Equal(t, userId, node.CreatedBy)

	// Verify auto-attachment to workspace
	workspaces, err := wsStore.GetByStorageNode(ctx, node.Uuid)
	assert.NoError(t, err)
	assert.Len(t, workspaces, 1)
	assert.Equal(t, testOrgId, workspaces[0].WorkspaceId)
	assert.True(t, workspaces[0].IsDefault)
}

func TestPostStorageNodeHandler_InvalidProviderType(t *testing.T) {
	accountStore, _, _ := setupStorageHandlerTest(t)
	ctx := context.Background()
	userId := "user-" + test.GenerateTestId()
	testAccount := createTestAccount(t, accountStore, userId)
	createTestWorkspaceEnablement(t, testAccount.Uuid, userId)

	reqBody, _ := json.Marshal(models.CreateStorageNodeRequest{
		AccountUuid:      testAccount.Uuid,
		OrganizationId:   testOrgId,
		Name:             "Bad Storage",
		StorageLocation:  "some-bucket",
		ProviderType:     "invalid",
		SkipProvisioning: true,
	})

	request := events.APIGatewayV2HTTPRequest{
		Body: string(reqBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, testOrgId),
		},
	}

	response, err := storage.PostStorageNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 400, response.StatusCode)
	assert.Contains(t, response.Body, "invalid provider type")
}

func TestPostStorageNodeHandler_MissingRequiredFields(t *testing.T) {
	_, _, _ = setupStorageHandlerTest(t)
	ctx := context.Background()

	reqBody, _ := json.Marshal(models.CreateStorageNodeRequest{
		Name: "Only Name",
	})

	request := events.APIGatewayV2HTTPRequest{
		Body: string(reqBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("test-user", ""),
		},
	}

	response, err := storage.PostStorageNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 400, response.StatusCode)
}

func TestPostStorageNodeHandler_AccountNotFound(t *testing.T) {
	_, _, _ = setupStorageHandlerTest(t)
	ctx := context.Background()

	reqBody, _ := json.Marshal(models.CreateStorageNodeRequest{
		AccountUuid:     uuid.New().String(),
		OrganizationId:  testOrgId,
		Name:            "Test Storage",
		StorageLocation: "bucket",
		ProviderType:    "s3",
	})

	request := events.APIGatewayV2HTTPRequest{
		Body: string(reqBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("test-user", testOrgId),
		},
	}

	response, err := storage.PostStorageNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 404, response.StatusCode)
}

func TestPostStorageNodeHandler_NonOwnerForbidden(t *testing.T) {
	accountStore, _, _ := setupStorageHandlerTest(t)
	ctx := context.Background()
	ownerId := "owner-" + test.GenerateTestId()
	testAccount := createTestAccount(t, accountStore, ownerId)
	createTestWorkspaceEnablement(t, testAccount.Uuid, ownerId)

	reqBody, _ := json.Marshal(models.CreateStorageNodeRequest{
		AccountUuid:     testAccount.Uuid,
		OrganizationId:  testOrgId,
		Name:            "Test Storage",
		StorageLocation: "bucket",
		ProviderType:    "s3",
	})

	request := events.APIGatewayV2HTTPRequest{
		Body: string(reqBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("random-user", testOrgId),
		},
	}

	response, err := storage.PostStorageNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 403, response.StatusCode)
}

func TestPostStorageNodeHandler_AzureBlob(t *testing.T) {
	accountStore, _, _ := setupStorageHandlerTest(t)
	ctx := context.Background()
	userId := "user-" + test.GenerateTestId()
	testAccount := createTestAccount(t, accountStore, userId)
	createTestWorkspaceEnablement(t, testAccount.Uuid, userId)

	reqBody, _ := json.Marshal(models.CreateStorageNodeRequest{
		AccountUuid:     testAccount.Uuid,
		OrganizationId:  testOrgId,
		Name:            "Azure Storage",
		StorageLocation: "https://myaccount.blob.core.windows.net/mycontainer",
		Region:          "eastus2",
		ProviderType:    "azure-blob",
	})

	request := events.APIGatewayV2HTTPRequest{
		Body: string(reqBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, testOrgId),
		},
	}

	response, err := storage.PostStorageNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 201, response.StatusCode)

	var node models.StorageNode
	err = json.Unmarshal([]byte(response.Body), &node)
	assert.NoError(t, err)
	assert.Equal(t, "azure-blob", node.ProviderType)
}

func TestPostStorageNodeHandler_NoEnablement(t *testing.T) {
	accountStore, _, _ := setupStorageHandlerTest(t)
	ctx := context.Background()
	userId := "user-" + test.GenerateTestId()
	testAccount := createTestAccount(t, accountStore, userId)
	// No workspace enablement created

	reqBody, _ := json.Marshal(models.CreateStorageNodeRequest{
		AccountUuid:      testAccount.Uuid,
		OrganizationId:   testOrgId,
		Name:             "Test Storage",
		StorageLocation:  "some-bucket",
		ProviderType:     "s3",
		SkipProvisioning: true,
	})

	request := events.APIGatewayV2HTTPRequest{
		Body: string(reqBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, testOrgId),
		},
	}

	response, err := storage.PostStorageNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 403, response.StatusCode)
	assert.Contains(t, response.Body, "not enabled for this workspace")
}

func TestPostStorageNodeHandler_MissingOrganizationId(t *testing.T) {
	_, _, _ = setupStorageHandlerTest(t)
	ctx := context.Background()

	reqBody, _ := json.Marshal(models.CreateStorageNodeRequest{
		AccountUuid:     uuid.New().String(),
		Name:            "Test Storage",
		StorageLocation: "some-bucket",
		ProviderType:    "s3",
	})

	request := events.APIGatewayV2HTTPRequest{
		Body: string(reqBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("test-user", testOrgId),
		},
	}

	response, err := storage.PostStorageNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 400, response.StatusCode)
}

func TestPostStorageNodeHandler_OwnerBypassesEnableStorage(t *testing.T) {
	accountStore, _, _ := setupStorageHandlerTest(t)
	ctx := context.Background()
	userId := "user-" + test.GenerateTestId()
	testAccount := createTestAccount(t, accountStore, userId)

	// Create enablement with enableStorage=false
	client := test.GetClient()
	wsStore := store_dynamodb.NewAccountWorkspaceStore(client, test.TEST_WORKSPACE_TABLE)
	err := wsStore.Insert(ctx, store_dynamodb.AccountWorkspace{
		AccountUuid:   testAccount.Uuid,
		WorkspaceId:   testOrgId,
		IsPublic:      true,
		EnableCompute: true,
		EnableStorage: false, // Storage disabled for admins
		EnabledBy:     userId,
		EnabledAt:     1000000000,
	})
	require.NoError(t, err)

	reqBody, _ := json.Marshal(models.CreateStorageNodeRequest{
		AccountUuid:      testAccount.Uuid,
		OrganizationId:   testOrgId,
		Name:             "Owner Storage",
		StorageLocation:  "owner-bucket",
		ProviderType:     "s3",
		SkipProvisioning: true,
	})

	request := events.APIGatewayV2HTTPRequest{
		Body: string(reqBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, testOrgId),
		},
	}

	// Owner should succeed even with enableStorage=false
	response, err := storage.PostStorageNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 201, response.StatusCode)
}

// --- GET /storage-nodes/{id} ---

func TestGetStorageNodeHandler_Success(t *testing.T) {
	accountStore, storageNodeStore, _ := setupStorageHandlerTest(t)
	ctx := context.Background()
	userId := "user-" + test.GenerateTestId()
	testAccount := createTestAccount(t, accountStore, userId)

	testNode := models.DynamoDBStorageNode{
		Uuid:            uuid.New().String(),
		Name:            "Test Storage",
		Description:     "Description",
		AccountUuid:     testAccount.Uuid,
		StorageLocation: "test-bucket",
		Region:          "us-east-1",
		ProviderType:    "s3",
		Status:          "Enabled",
		CreatedAt:       "2024-01-01T00:00:00Z",
		CreatedBy:       userId,
	}
	err := storageNodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": testNode.Uuid},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, ""),
		},
	}

	response, err := storage.GetStorageNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 200, response.StatusCode)

	var node models.StorageNode
	err = json.Unmarshal([]byte(response.Body), &node)
	assert.NoError(t, err)
	assert.Equal(t, testNode.Uuid, node.Uuid)
	assert.Equal(t, "Test Storage", node.Name)
	assert.Equal(t, "test-bucket", node.StorageLocation)
}

func TestGetStorageNodeHandler_NotFound(t *testing.T) {
	_, _, _ = setupStorageHandlerTest(t)
	ctx := context.Background()

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": uuid.New().String()},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("test-user", ""),
		},
	}

	response, err := storage.GetStorageNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 404, response.StatusCode)
}

func TestGetStorageNodeHandler_WorkspaceMemberAccess(t *testing.T) {
	accountStore, storageNodeStore, wsStore := setupStorageHandlerTest(t)
	ctx := context.Background()
	ownerId := "owner-" + test.GenerateTestId()
	memberId := "member-" + test.GenerateTestId()
	orgId := "N:organization:" + uuid.New().String()
	testAccount := createTestAccount(t, accountStore, ownerId)

	testNode := models.DynamoDBStorageNode{
		Uuid:            uuid.New().String(),
		Name:            "Shared Storage",
		AccountUuid:     testAccount.Uuid,
		StorageLocation: "shared-bucket",
		ProviderType:    "s3",
		Status:          "Enabled",
		CreatedAt:       "2024-01-01T00:00:00Z",
		CreatedBy:       ownerId,
	}
	err := storageNodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	// Attach to workspace
	err = wsStore.Insert(ctx, models.DynamoDBStorageNodeWorkspace{
		StorageNodeUuid: testNode.Uuid,
		WorkspaceId:     orgId,
		EnabledBy:       ownerId,
		EnabledAt:       "2024-01-01T00:00:00Z",
	})
	require.NoError(t, err)

	// Workspace member can access
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": testNode.Uuid},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(memberId, orgId),
		},
	}

	response, err := storage.GetStorageNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 200, response.StatusCode)
}

// --- GET /storage-nodes (list) ---

func TestGetStorageNodesHandler_AccountOwnerMode(t *testing.T) {
	accountStore, storageNodeStore, _ := setupStorageHandlerTest(t)
	ctx := context.Background()
	userId := "user-" + test.GenerateTestId()
	testAccount := createTestAccount(t, accountStore, userId)

	// Create two storage nodes
	for i := 0; i < 2; i++ {
		node := models.DynamoDBStorageNode{
			Uuid:            uuid.New().String(),
			Name:            "Storage " + test.GenerateTestId(),
			AccountUuid:     testAccount.Uuid,
			StorageLocation: "bucket-" + test.GenerateTestId(),
			ProviderType:    "s3",
			Status:          "Enabled",
			CreatedAt:       "2024-01-01T00:00:00Z",
			CreatedBy:       userId,
		}
		err := storageNodeStore.Put(ctx, node)
		require.NoError(t, err)
	}

	request := events.APIGatewayV2HTTPRequest{
		QueryStringParameters: map[string]string{"account_owner": "true"},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, ""),
		},
	}

	response, err := storage.GetStorageNodesHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 200, response.StatusCode)

	var nodes []models.StorageNode
	err = json.Unmarshal([]byte(response.Body), &nodes)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(nodes), 2)
}

func TestGetStorageNodesHandler_BadRequest_NoFilter(t *testing.T) {
	_, _, _ = setupStorageHandlerTest(t)
	ctx := context.Background()

	request := events.APIGatewayV2HTTPRequest{
		QueryStringParameters: map[string]string{},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("test-user", ""),
		},
	}

	response, err := storage.GetStorageNodesHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 400, response.StatusCode)
}

// --- PATCH /storage-nodes/{id} ---

func TestPatchStorageNodeHandler_UpdateName(t *testing.T) {
	accountStore, storageNodeStore, _ := setupStorageHandlerTest(t)
	ctx := context.Background()
	userId := "user-" + test.GenerateTestId()
	testAccount := createTestAccount(t, accountStore, userId)

	testNode := models.DynamoDBStorageNode{
		Uuid:            uuid.New().String(),
		Name:            "Original Name",
		Description:     "Original Desc",
		AccountUuid:     testAccount.Uuid,
		StorageLocation: "my-bucket",
		ProviderType:    "s3",
		Status:          "Enabled",
		CreatedAt:       "2024-01-01T00:00:00Z",
		CreatedBy:       userId,
	}
	err := storageNodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	newName := "Updated Name"
	reqBody, _ := json.Marshal(models.StorageNodeUpdateRequest{
		Name: &newName,
	})

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": testNode.Uuid},
		Body:           string(reqBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, ""),
		},
	}

	response, err := storage.PatchStorageNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 200, response.StatusCode)

	var node models.StorageNode
	err = json.Unmarshal([]byte(response.Body), &node)
	assert.NoError(t, err)
	assert.Equal(t, "Updated Name", node.Name)
	assert.Equal(t, "Original Desc", node.Description)
}

func TestPatchStorageNodeHandler_InvalidStatus(t *testing.T) {
	accountStore, storageNodeStore, _ := setupStorageHandlerTest(t)
	ctx := context.Background()
	userId := "user-" + test.GenerateTestId()
	testAccount := createTestAccount(t, accountStore, userId)

	testNode := models.DynamoDBStorageNode{
		Uuid:        uuid.New().String(),
		AccountUuid: testAccount.Uuid,
		ProviderType: "s3",
		Status:      "Enabled",
		CreatedBy:   userId,
	}
	err := storageNodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	invalidStatus := "Invalid"
	reqBody, _ := json.Marshal(models.StorageNodeUpdateRequest{
		Status: &invalidStatus,
	})

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": testNode.Uuid},
		Body:           string(reqBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, ""),
		},
	}

	response, err := storage.PatchStorageNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 400, response.StatusCode)
}

func TestPatchStorageNodeHandler_NonOwnerForbidden(t *testing.T) {
	accountStore, storageNodeStore, _ := setupStorageHandlerTest(t)
	ctx := context.Background()
	ownerId := "owner-" + test.GenerateTestId()
	testAccount := createTestAccount(t, accountStore, ownerId)

	testNode := models.DynamoDBStorageNode{
		Uuid:        uuid.New().String(),
		AccountUuid: testAccount.Uuid,
		ProviderType: "s3",
		Status:      "Enabled",
		CreatedBy:   ownerId,
	}
	err := storageNodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	newName := "Hacked"
	reqBody, _ := json.Marshal(models.StorageNodeUpdateRequest{Name: &newName})

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": testNode.Uuid},
		Body:           string(reqBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("random-user", ""),
		},
	}

	response, err := storage.PatchStorageNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 403, response.StatusCode)
}

// --- DELETE /storage-nodes/{id} ---

func TestDeleteStorageNodeHandler_Success(t *testing.T) {
	accountStore, storageNodeStore, _ := setupStorageHandlerTest(t)
	ctx := context.Background()
	userId := "user-" + test.GenerateTestId()
	testAccount := createTestAccount(t, accountStore, userId)

	testNode := models.DynamoDBStorageNode{
		Uuid:            uuid.New().String(),
		Name:            "To Delete",
		AccountUuid:     testAccount.Uuid,
		StorageLocation: "delete-bucket",
		ProviderType:    "s3",
		Status:          "Enabled",
		CreatedBy:       userId,
	}
	err := storageNodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": testNode.Uuid},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, ""),
		},
	}

	response, err := storage.DeleteStorageNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 200, response.StatusCode)

	// Verify deletion
	deleted, err := storageNodeStore.GetById(ctx, testNode.Uuid)
	assert.NoError(t, err)
	assert.Empty(t, deleted.Uuid)
}

func TestDeleteStorageNodeHandler_NonOwnerForbidden(t *testing.T) {
	accountStore, storageNodeStore, _ := setupStorageHandlerTest(t)
	ctx := context.Background()
	ownerId := "owner-" + test.GenerateTestId()
	testAccount := createTestAccount(t, accountStore, ownerId)

	testNode := models.DynamoDBStorageNode{
		Uuid:        uuid.New().String(),
		AccountUuid: testAccount.Uuid,
		ProviderType: "s3",
		Status:      "Enabled",
		CreatedBy:   ownerId,
	}
	err := storageNodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": testNode.Uuid},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("random-user", ""),
		},
	}

	response, err := storage.DeleteStorageNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 403, response.StatusCode)
}

func TestDeleteStorageNodeHandler_NotFound(t *testing.T) {
	_, _, _ = setupStorageHandlerTest(t)
	ctx := context.Background()

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": uuid.New().String()},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("test-user", ""),
		},
	}

	response, err := storage.DeleteStorageNodeHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 404, response.StatusCode)
}

// --- POST /storage-nodes/{id}/workspace ---

func TestAttachToWorkspaceHandler_Success(t *testing.T) {
	accountStore, storageNodeStore, wsStore := setupStorageHandlerTest(t)
	ctx := context.Background()
	userId := "user-" + test.GenerateTestId()
	orgId := "N:organization:" + uuid.New().String()
	testAccount := createTestAccount(t, accountStore, userId)

	testNode := models.DynamoDBStorageNode{
		Uuid:        uuid.New().String(),
		AccountUuid: testAccount.Uuid,
		ProviderType: "s3",
		Status:      "Enabled",
		CreatedBy:   userId,
	}
	err := storageNodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	reqBody, _ := json.Marshal(storage.AttachWorkspaceRequest{
		WorkspaceId: orgId,
		IsDefault:   true,
	})

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": testNode.Uuid},
		Body:           string(reqBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, orgId),
		},
	}

	response, err := storage.AttachToWorkspaceHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 201, response.StatusCode)

	// Verify in DynamoDB
	ws, err := wsStore.Get(ctx, testNode.Uuid, orgId)
	assert.NoError(t, err)
	assert.Equal(t, testNode.Uuid, ws.StorageNodeUuid)
	assert.Equal(t, orgId, ws.WorkspaceId)
	assert.True(t, ws.IsDefault)
}

func TestAttachToWorkspaceHandler_AlreadyAttached(t *testing.T) {
	accountStore, storageNodeStore, wsStore := setupStorageHandlerTest(t)
	ctx := context.Background()
	userId := "user-" + test.GenerateTestId()
	orgId := "N:organization:" + uuid.New().String()
	testAccount := createTestAccount(t, accountStore, userId)

	testNode := models.DynamoDBStorageNode{
		Uuid:        uuid.New().String(),
		AccountUuid: testAccount.Uuid,
		ProviderType: "s3",
		Status:      "Enabled",
		CreatedBy:   userId,
	}
	err := storageNodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	// Pre-attach
	err = wsStore.Insert(ctx, models.DynamoDBStorageNodeWorkspace{
		StorageNodeUuid: testNode.Uuid,
		WorkspaceId:     orgId,
		EnabledBy:       userId,
	})
	require.NoError(t, err)

	reqBody, _ := json.Marshal(storage.AttachWorkspaceRequest{
		WorkspaceId: orgId,
	})

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": testNode.Uuid},
		Body:           string(reqBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, orgId),
		},
	}

	response, err := storage.AttachToWorkspaceHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 409, response.StatusCode)
}

// --- DELETE /storage-nodes/{id}/workspace ---

func TestDetachFromWorkspaceHandler_Success(t *testing.T) {
	accountStore, storageNodeStore, wsStore := setupStorageHandlerTest(t)
	ctx := context.Background()
	userId := "user-" + test.GenerateTestId()
	orgId := "N:organization:" + uuid.New().String()
	testAccount := createTestAccount(t, accountStore, userId)

	testNode := models.DynamoDBStorageNode{
		Uuid:        uuid.New().String(),
		AccountUuid: testAccount.Uuid,
		ProviderType: "s3",
		Status:      "Enabled",
		CreatedBy:   userId,
	}
	err := storageNodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	err = wsStore.Insert(ctx, models.DynamoDBStorageNodeWorkspace{
		StorageNodeUuid: testNode.Uuid,
		WorkspaceId:     orgId,
		EnabledBy:       userId,
	})
	require.NoError(t, err)

	reqBody, _ := json.Marshal(storage.DetachWorkspaceRequest{
		WorkspaceId: orgId,
	})

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": testNode.Uuid},
		Body:           string(reqBody),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, orgId),
		},
	}

	response, err := storage.DetachFromWorkspaceHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 200, response.StatusCode)

	// Verify detachment
	ws, err := wsStore.Get(ctx, testNode.Uuid, orgId)
	assert.NoError(t, err)
	assert.Empty(t, ws.StorageNodeUuid)
}

// --- GET /storage-nodes/{id}/impact ---

func TestGetDetachImpactHandler_Success(t *testing.T) {
	accountStore, storageNodeStore, _ := setupStorageHandlerTest(t)
	ctx := context.Background()
	userId := "user-" + test.GenerateTestId()
	testAccount := createTestAccount(t, accountStore, userId)

	testNode := models.DynamoDBStorageNode{
		Uuid:        uuid.New().String(),
		AccountUuid: testAccount.Uuid,
		ProviderType: "s3",
		Status:      "Enabled",
		CreatedBy:   userId,
	}
	err := storageNodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": testNode.Uuid},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(userId, ""),
		},
	}

	response, err := storage.GetDetachImpactHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 200, response.StatusCode)

	var impact storage.ImpactResponse
	err = json.Unmarshal([]byte(response.Body), &impact)
	assert.NoError(t, err)
	assert.Equal(t, 0, impact.Datasets)
	assert.Equal(t, 0, impact.Files)
	assert.Equal(t, int64(0), impact.TotalSizeBytes)
}

func TestGetDetachImpactHandler_NonOwnerForbidden(t *testing.T) {
	accountStore, storageNodeStore, _ := setupStorageHandlerTest(t)
	ctx := context.Background()
	ownerId := "owner-" + test.GenerateTestId()
	testAccount := createTestAccount(t, accountStore, ownerId)

	testNode := models.DynamoDBStorageNode{
		Uuid:        uuid.New().String(),
		AccountUuid: testAccount.Uuid,
		ProviderType: "s3",
		Status:      "Enabled",
		CreatedBy:   ownerId,
	}
	err := storageNodeStore.Put(ctx, testNode)
	require.NoError(t, err)

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": testNode.Uuid},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer("random-user", ""),
		},
	}

	response, err := storage.GetDetachImpactHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 403, response.StatusCode)
}
