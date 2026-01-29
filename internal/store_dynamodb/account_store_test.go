package store_dynamodb_test

import (
    "context"
    "github.com/pennsieve/account-service/internal/test"
    "testing"

    "github.com/google/uuid"
    "github.com/pennsieve/account-service/internal/store_dynamodb"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func setupAccountStoreTest(t *testing.T) *store_dynamodb.AccountDatabaseStore {
    // Clear data from previous tests
    // Don't clear all test data - use unique IDs to avoid conflicts with other tests

    // Return store using shared table
    store := store_dynamodb.NewAccountDatabaseStore(test.GetTestClient(), TEST_ACCOUNTS_TABLE).(*store_dynamodb.AccountDatabaseStore)
    return store
}

func TestAccountStore_Insert(t *testing.T) {
    store := setupAccountStoreTest(t)
    accountUuid := uuid.New().String()

    account := store_dynamodb.Account{
        Uuid:        accountUuid,
        UserId:      "user123",
        AccountId:   "account456",
        AccountType: "aws",
        RoleName:    "TestRole",
        ExternalId:  "external789",
        Name:        "Test Account",
        Description: "Test Description",
        Status:      "Enabled",
    }

    // Test Insert
    err := store.Insert(context.Background(), account)
    assert.NoError(t, err, "Insert should not return error")

    // Verify the account was inserted
    retrievedAccount, err := store.GetById(context.Background(), accountUuid)
    require.NoError(t, err, "GetById should not return error")

    assert.Equal(t, account.Uuid, retrievedAccount.Uuid)
    assert.Equal(t, account.UserId, retrievedAccount.UserId)
    assert.Equal(t, account.AccountId, retrievedAccount.AccountId)
    assert.Equal(t, account.AccountType, retrievedAccount.AccountType)
    assert.Equal(t, account.RoleName, retrievedAccount.RoleName)
    assert.Equal(t, account.ExternalId, retrievedAccount.ExternalId)
    assert.Equal(t, account.Name, retrievedAccount.Name)
    assert.Equal(t, account.Description, retrievedAccount.Description)
    assert.Equal(t, account.Status, retrievedAccount.Status)
}

func TestAccountStore_GetById_NotFound(t *testing.T) {
    store := setupAccountStoreTest(t)
    nonExistentId := uuid.New().String()

    // Test GetById with non-existent ID
    account, err := store.GetById(context.Background(), nonExistentId)
    assert.NoError(t, err, "GetById should not return error for non-existent item")
    assert.Empty(t, account.Uuid, "Account should be empty for non-existent item")
}

func TestAccountStore_Get_FilterByOrganization(t *testing.T) {
    store := setupAccountStoreTest(t)
    orgId := "org-123"

    // Insert regular accounts (current account structure doesn't have organizationId field)
    account1 := store_dynamodb.Account{
        Uuid:        uuid.New().String(),
        UserId:      "user1",
        AccountId:   "account1",
        AccountType: "aws",
        RoleName:    "Role1",
        ExternalId:  "external1",
        Status:      "Enabled",
    }
    account2 := store_dynamodb.Account{
        Uuid:        uuid.New().String(),
        UserId:      "user2",
        AccountId:   "account2",
        AccountType: "gcp",
        RoleName:    "Role2",
        ExternalId:  "external2",
        Status:      "Enabled",
    }

    err := store.Insert(context.Background(), account1)
    require.NoError(t, err, "Failed to insert account1")
    err = store.Insert(context.Background(), account2)
    require.NoError(t, err, "Failed to insert account2")

    // Test Get with organization filter
    // Since current accounts don't have organizationId field, this will return empty results
    // This tests the legacy migration path of the Get method
    accounts, err := store.Get(context.Background(), orgId, map[string]string{})
    assert.NoError(t, err, "Get should not return error")

    // Should return no accounts since current account structure doesn't have organizationId
    // This is expected behavior for the legacy migration method
    assert.Len(t, accounts, 0, "Should return no accounts as current structure doesn't have organizationId field")
}

func TestAccountStore_Update(t *testing.T) {
    store := setupAccountStoreTest(t)
    accountUuid := uuid.New().String()

    // Insert initial account
    originalAccount := store_dynamodb.Account{
        Uuid:        accountUuid,
        UserId:      "user123",
        AccountId:   "account456",
        AccountType: "aws",
        RoleName:    "OriginalRole",
        ExternalId:  "original789",
        Name:        "Original Account",
        Description: "Original Description",
        Status:      "Enabled",
    }

    err := store.Insert(context.Background(), originalAccount)
    require.NoError(t, err, "Failed to insert original account")

    // Update account
    updatedAccount := originalAccount
    updatedAccount.Name = "Updated Account"
    updatedAccount.Description = "Updated Description"
    updatedAccount.Status = "Paused"
    updatedAccount.RoleName = "UpdatedRole"

    err = store.Update(context.Background(), updatedAccount)
    assert.NoError(t, err, "Update should not return error")

    // Verify the account was updated
    retrievedAccount, err := store.GetById(context.Background(), accountUuid)
    require.NoError(t, err, "GetById should not return error")

    assert.Equal(t, updatedAccount.Uuid, retrievedAccount.Uuid)
    assert.Equal(t, updatedAccount.UserId, retrievedAccount.UserId)
    assert.Equal(t, updatedAccount.AccountId, retrievedAccount.AccountId)
    assert.Equal(t, updatedAccount.AccountType, retrievedAccount.AccountType)
    assert.Equal(t, updatedAccount.RoleName, retrievedAccount.RoleName)
    assert.Equal(t, updatedAccount.ExternalId, retrievedAccount.ExternalId)
    assert.Equal(t, updatedAccount.Name, retrievedAccount.Name)
    assert.Equal(t, updatedAccount.Description, retrievedAccount.Description)
    assert.Equal(t, updatedAccount.Status, retrievedAccount.Status)
}

func TestAccountStore_Update_NonExistent(t *testing.T) {
    store := setupAccountStoreTest(t)

    // Try to update non-existent account
    nonExistentAccount := store_dynamodb.Account{
        Uuid:        uuid.New().String(),
        UserId:      "user123",
        AccountId:   "account456",
        AccountType: "aws",
        RoleName:    "TestRole",
        ExternalId:  "external789",
        Name:        "Non-existent Account",
        Status:      "Enabled",
    }

    err := store.Update(context.Background(), nonExistentAccount)
    // The method will upsert the new record.

    assert.Nil(t, err, "Update should not return error")
}
