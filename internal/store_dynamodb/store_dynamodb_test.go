package store_dynamodb_test

import (
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"
    "github.com/pennsieve/account-service/internal/test"
    "log"
    "os"
    "testing"
)

// Shared table names for all store tests
const (
    TEST_ACCOUNTS_TABLE            = "test-accounts-table"
    TEST_ACCOUNTS_WITH_INDEX_TABLE = "test-accounts-with-index-table"
    TEST_NODES_TABLE               = "test-nodes-table"
    TEST_ACCESS_TABLE              = "test-access-table"
    TEST_WORKSPACE_TABLE           = "test-workspace-table"
)

var globalTestClient *dynamodb.Client

// TestMain sets up tables once for the entire store_dynamodb package
func TestMain(m *testing.M) {
    // Setup: Create client and tables
    globalTestClient = test.GetClient()

    if err := test.SetupPackageTables(); err != nil {
        log.Fatalf("Failed to setup package tables: %v", err)
    }

    // Run all tests - individual tests clean up their own data with unique IDs
    exitCode := m.Run()

    os.Exit(exitCode)
}
