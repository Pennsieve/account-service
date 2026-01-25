package store_dynamodb_test

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func CreateWorkspaceEnablementTable(dynamoDBClient *dynamodb.Client, tableName string) error {
	_, err := dynamoDBClient.CreateTable(context.TODO(), &dynamodb.CreateTableInput{
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
		GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
			{
				IndexName: aws.String("workspaceId-index"),
				KeySchema: []types.KeySchemaElement{
					{
						AttributeName: aws.String("workspaceId"),
						KeyType:       types.KeyTypeHash,
					},
				},
				Projection: &types.Projection{
					ProjectionType: types.ProjectionTypeAll,
				},
			},
		},
		TableName:   aws.String(tableName),
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		return err
	}

	// Wait for table to be created
	waiter := dynamodb.NewTableExistsWaiter(dynamoDBClient)
	err = waiter.Wait(context.TODO(), &dynamodb.DescribeTableInput{
		TableName: aws.String(tableName),
	}, 5*time.Minute)

	return err
}

func TestWorkspaceEnablementInsertAndGet(t *testing.T) {
	tableName := "account-workspace-enablement-test"
	dynamoDBClient := getClient()

	// Create table
	err := CreateWorkspaceEnablementTable(dynamoDBClient, tableName)
	require.NoError(t, err, "Failed to create workspace enablement table")
	defer DeleteTable(dynamoDBClient, tableName)

	store := store_dynamodb.NewAccountWorkspaceStore(dynamoDBClient, tableName)

	// Test data
	accountUuid := uuid.New().String()
	organizationId := "org-123"
	userId := "user-456"

	enablement := store_dynamodb.AccountWorkspace{
		AccountUuid:    accountUuid,
		WorkspaceId:    organizationId,  // Use WorkspaceId instead of OrganizationId
		IsPublic:       true,
		EnabledBy:      userId,
		EnabledAt:      time.Now().Unix(),
	}

	// Test Insert
	err = store.Insert(context.Background(), enablement)
	assert.NoError(t, err, "Failed to insert enablement")

	// Test Get
	retrieved, err := store.Get(context.Background(), accountUuid, organizationId)
	assert.NoError(t, err, "Failed to get enablement")
	assert.Equal(t, accountUuid, retrieved.AccountUuid)
	assert.Equal(t, organizationId, retrieved.WorkspaceId)  // Check WorkspaceId field
	assert.Equal(t, true, retrieved.IsPublic)
	assert.Equal(t, userId, retrieved.EnabledBy)

	// Test Get non-existent
	nonExistent, err := store.Get(context.Background(), "non-existent", "non-existent")
	assert.NoError(t, err, "Get should not error for non-existent item")
	assert.Empty(t, nonExistent.AccountUuid, "Non-existent item should return empty")
}

func TestWorkspaceEnablementGetByAccount(t *testing.T) {
	tableName := "account-workspace-enablement-test"
	dynamoDBClient := getClient()

	// Create table
	err := CreateWorkspaceEnablementTable(dynamoDBClient, tableName)
	require.NoError(t, err, "Failed to create workspace enablement table")
	defer DeleteTable(dynamoDBClient, tableName)

	store := store_dynamodb.NewAccountWorkspaceStore(dynamoDBClient, tableName)

	// Test data - one account enabled for multiple workspaces
	accountUuid := uuid.New().String()
	userId := "user-123"
	organizations := []string{"org-1", "org-2", "org-3"}

	// Insert enablements for multiple organizations
	for i, orgId := range organizations {
		enablement := store_dynamodb.AccountWorkspace{
			AccountUuid:    accountUuid,
			WorkspaceId: orgId,
			IsPublic:       i%2 == 0, // Alternate public/private
			EnabledBy:      userId,
			EnabledAt:      time.Now().Unix(),
		}
		err := store.Insert(context.Background(), enablement)
		require.NoError(t, err, "Failed to insert enablement for org %s", orgId)
	}

	// Test GetByAccount
	enablements, err := store.GetByAccount(context.Background(), accountUuid)
	assert.NoError(t, err, "Failed to get enablements by account")
	assert.Len(t, enablements, 3, "Should have 3 enablements")

	// Verify all organizations are present
	orgMap := make(map[string]bool)
	for _, e := range enablements {
		orgMap[e.WorkspaceId] = true
		assert.Equal(t, accountUuid, e.AccountUuid)
	}
	for _, orgId := range organizations {
		assert.True(t, orgMap[orgId], "Organization %s should be in results", orgId)
	}
}

func TestWorkspaceEnablementGetByOrganization(t *testing.T) {
	tableName := "account-workspace-enablement-test"
	dynamoDBClient := getClient()

	// Create table
	err := CreateWorkspaceEnablementTable(dynamoDBClient, tableName)
	require.NoError(t, err, "Failed to create workspace enablement table")
	defer DeleteTable(dynamoDBClient, tableName)

	store := store_dynamodb.NewAccountWorkspaceStore(dynamoDBClient, tableName)

	// Test data - multiple accounts enabled for one workspace
	organizationId := "org-shared"
	userId := "user-admin"
	accountUuids := []string{uuid.New().String(), uuid.New().String(), uuid.New().String()}

	// Insert enablements for multiple accounts
	for i, accUuid := range accountUuids {
		enablement := store_dynamodb.AccountWorkspace{
			AccountUuid:    accUuid,
			WorkspaceId: organizationId,
			IsPublic:       i == 0, // Only first is public
			EnabledBy:      userId,
			EnabledAt:      time.Now().Unix(),
		}
		err := store.Insert(context.Background(), enablement)
		require.NoError(t, err, "Failed to insert enablement for account %s", accUuid)
	}

	// Test GetByWorkspace
	enablements, err := store.GetByWorkspace(context.Background(), organizationId)
	assert.NoError(t, err, "Failed to get enablements by organization")
	assert.Len(t, enablements, 3, "Should have 3 enablements")

	// Verify all accounts are present
	accountMap := make(map[string]bool)
	publicCount := 0
	for _, e := range enablements {
		accountMap[e.AccountUuid] = true
		assert.Equal(t, organizationId, e.WorkspaceId)
		if e.IsPublic {
			publicCount++
		}
	}
	for _, accUuid := range accountUuids {
		assert.True(t, accountMap[accUuid], "Account %s should be in results", accUuid)
	}
	assert.Equal(t, 1, publicCount, "Should have exactly 1 public account")
}

func TestWorkspaceEnablementDelete(t *testing.T) {
	tableName := "account-workspace-enablement-test"
	dynamoDBClient := getClient()

	// Create table
	err := CreateWorkspaceEnablementTable(dynamoDBClient, tableName)
	require.NoError(t, err, "Failed to create workspace enablement table")
	defer DeleteTable(dynamoDBClient, tableName)

	store := store_dynamodb.NewAccountWorkspaceStore(dynamoDBClient, tableName)

	// Test data
	accountUuid := uuid.New().String()
	organizationId := "org-to-delete"
	userId := "user-123"

	enablement := store_dynamodb.AccountWorkspace{
		AccountUuid:    accountUuid,
		WorkspaceId: organizationId,
		IsPublic:       false,
		EnabledBy:      userId,
		EnabledAt:      time.Now().Unix(),
	}

	// Insert enablement
	err = store.Insert(context.Background(), enablement)
	require.NoError(t, err, "Failed to insert enablement")

	// Verify it exists
	retrieved, err := store.Get(context.Background(), accountUuid, organizationId)
	assert.NoError(t, err)
	assert.Equal(t, accountUuid, retrieved.AccountUuid)

	// Delete enablement
	err = store.Delete(context.Background(), accountUuid, organizationId)
	assert.NoError(t, err, "Failed to delete enablement")

	// Verify it's deleted
	deleted, err := store.Get(context.Background(), accountUuid, organizationId)
	assert.NoError(t, err, "Get should not error for deleted item")
	assert.Empty(t, deleted.AccountUuid, "Deleted item should return empty")

	// Delete non-existent should not error
	err = store.Delete(context.Background(), "non-existent", "non-existent")
	assert.NoError(t, err, "Delete should not error for non-existent item")
}

func TestWorkspaceEnablementPrivatePublicFlag(t *testing.T) {
	tableName := "account-workspace-enablement-test"
	dynamoDBClient := getClient()

	// Create table
	err := CreateWorkspaceEnablementTable(dynamoDBClient, tableName)
	require.NoError(t, err, "Failed to create workspace enablement table")
	defer DeleteTable(dynamoDBClient, tableName)

	store := store_dynamodb.NewAccountWorkspaceStore(dynamoDBClient, tableName)

	accountUuid := uuid.New().String()

	// Test data - same account for different workspaces with different privacy settings
	testCases := []struct {
		orgId    string
		isPublic bool
	}{
		{"org-public", true},
		{"org-private", false},
		{"org-also-public", true},
	}

	// Insert with different privacy settings
	for _, tc := range testCases {
		enablement := store_dynamodb.AccountWorkspace{
			AccountUuid:    accountUuid,
			WorkspaceId: tc.orgId,
			IsPublic:       tc.isPublic,
			EnabledBy:      "user-123",
			EnabledAt:      time.Now().Unix(),
		}
		err := store.Insert(context.Background(), enablement)
		require.NoError(t, err, "Failed to insert enablement for %s", tc.orgId)
	}

	// Verify each has correct privacy setting
	for _, tc := range testCases {
		retrieved, err := store.Get(context.Background(), accountUuid, tc.orgId)
		assert.NoError(t, err)
		assert.Equal(t, tc.isPublic, retrieved.IsPublic,
			"Privacy setting for %s should be %v", tc.orgId, tc.isPublic)
	}

	// Get all for account and verify mixed privacy settings
	enablements, err := store.GetByAccount(context.Background(), accountUuid)
	assert.NoError(t, err)
	assert.Len(t, enablements, 3)

	publicCount := 0
	for _, e := range enablements {
		if e.IsPublic {
			publicCount++
		}
	}
	assert.Equal(t, 2, publicCount, "Should have 2 public and 1 private enablement")
}
