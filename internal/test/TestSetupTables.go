package test

import (
    "context"
    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/credentials"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
    "github.com/google/uuid"
    "github.com/pennsieve/account-service/internal/store_dynamodb"
    "github.com/stretchr/testify/require"
    "log"
    "os"
    "strings"
    "testing"
    "time"
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

func SetupPackageTables() error {
    // Initialize client if not already done
    if globalTestClient == nil {
        globalTestClient = GetClient()
    }
    
    tables := []struct {
        name   string
        create func() error
    }{
        {TEST_ACCOUNTS_TABLE, func() error { 
            _, err := CreateAccountsTable(globalTestClient, TEST_ACCOUNTS_TABLE)
            // Ignore table exists errors
            if err != nil && strings.Contains(err.Error(), "ResourceInUseException") {
                return nil
            }
            return err 
        }},
        {TEST_ACCOUNTS_WITH_INDEX_TABLE, func() error { 
            _, err := CreateAccountsTableWithUserIndex(globalTestClient, TEST_ACCOUNTS_WITH_INDEX_TABLE)
            // Ignore table exists errors  
            if err != nil && strings.Contains(err.Error(), "ResourceInUseException") {
                return nil
            }
            return err
        }},
        {TEST_NODES_TABLE, createSharedNodesTable},
        {TEST_ACCESS_TABLE, createSharedAccessTable},
        {TEST_WORKSPACE_TABLE, createSharedWorkspaceTable},
    }

    for _, table := range tables {
        // Check if table exists and recreate if needed
        if err := ensureTableFreshState(globalTestClient, table.name, table.create); err != nil {
            return err
        }
    }

    return nil
}

func CleanupPackageTables() {
    tables := []string{
        TEST_ACCOUNTS_TABLE,
        TEST_ACCOUNTS_WITH_INDEX_TABLE,
        TEST_NODES_TABLE,
        TEST_ACCESS_TABLE,
        TEST_WORKSPACE_TABLE,
    }

    for _, tableName := range tables {
        _ = DeleteTable(globalTestClient, tableName)
    }
    log.Println("Deleted all package tables")
}

// ClearTestData clears all data from test tables
// Exported so handler tests can reuse this functionality
func ClearTestData() error {
    tables := []string{
        TEST_ACCOUNTS_TABLE,
        TEST_ACCOUNTS_WITH_INDEX_TABLE,
        TEST_NODES_TABLE,
        TEST_ACCESS_TABLE,
        TEST_WORKSPACE_TABLE,
    }

    for _, tableName := range tables {
        if err := ClearStoreDynamoDBTable(globalTestClient, tableName); err != nil {
            return err
        }
    }
    return nil
}

func createSharedNodesTable() error {
    _, err := globalTestClient.CreateTable(context.TODO(), &dynamodb.CreateTableInput{
        AttributeDefinitions: []types.AttributeDefinition{
            {
                AttributeName: aws.String("uuid"),
                AttributeType: types.ScalarAttributeTypeS,
            },
            {
                AttributeName: aws.String("organizationId"),
                AttributeType: types.ScalarAttributeTypeS,
            },
        },
        KeySchema: []types.KeySchemaElement{
            {
                AttributeName: aws.String("uuid"),
                KeyType:       types.KeyTypeHash,
            },
        },
        GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
            {
                IndexName: aws.String("organizationId-index"),
                KeySchema: []types.KeySchemaElement{
                    {
                        AttributeName: aws.String("organizationId"),
                        KeyType:       types.KeyTypeHash,
                    },
                },
                Projection: &types.Projection{
                    ProjectionType: types.ProjectionTypeAll,
                },
            },
        },
        TableName:   aws.String(TEST_NODES_TABLE),
        BillingMode: types.BillingModePayPerRequest,
    })
    return err
}

func createSharedAccessTable() error {
    _, err := globalTestClient.CreateTable(context.TODO(), &dynamodb.CreateTableInput{
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
        TableName:   aws.String(TEST_ACCESS_TABLE),
        BillingMode: types.BillingModePayPerRequest,
    })
    return err
}

func createSharedWorkspaceTable() error {
    _, err := globalTestClient.CreateTable(context.TODO(), &dynamodb.CreateTableInput{
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
        TableName:   aws.String(TEST_WORKSPACE_TABLE),
        BillingMode: types.BillingModePayPerRequest,
    })
    return err
}

// Helper functions for tests
func GetTestClient() *dynamodb.Client {
    return globalTestClient
}

// ClearStoreDynamoDBTable clears data from a specific table using proper key schema
// Exported so handler tests can reuse this functionality
func ClearStoreDynamoDBTable(client *dynamodb.Client, tableName string) error {
    // Scan the table to get all items
    scanResult, err := client.Scan(context.TODO(), &dynamodb.ScanInput{
        TableName: aws.String(tableName),
    })
    if err != nil {
        return err
    }

    // Delete all items - handle different table key schemas
    for _, item := range scanResult.Items {
        var key map[string]types.AttributeValue

        switch tableName {
        case TEST_ACCESS_TABLE:
            // Access table has composite key: entityId (hash) + nodeId (range)
            key = map[string]types.AttributeValue{
                "entityId": item["entityId"],
                "nodeId":   item["nodeId"],
            }
        case TEST_WORKSPACE_TABLE:
            // Workspace table has composite key: accountUuid (hash) + workspaceId (range)
            key = map[string]types.AttributeValue{
                "accountUuid": item["accountUuid"],
                "workspaceId": item["workspaceId"],
            }
        default:
            // Accounts and nodes tables have single key: uuid
            key = map[string]types.AttributeValue{
                "uuid": item["uuid"],
            }
        }

        _, err = client.DeleteItem(context.TODO(), &dynamodb.DeleteItemInput{
            TableName: aws.String(tableName),
            Key:       key,
        })
        if err != nil {
            return err
        }
    }

    return nil
}

func isStoreDynamoDBTableExistsError(err error) bool {
    return err.Error() != "" && (
        err.Error() == "ResourceInUseException: Cannot create preexisting table" ||
            strings.Contains(err.Error(), "ResourceInUseException") ||
            strings.Contains(err.Error(), "preexisting table"))
}

func setupStoreDynamoDBTest(t *testing.T, withUserIndex bool) (*dynamodb.Client, *store_dynamodb.AccountDatabaseStore) {
    // Clear data from previous tests
    require.NoError(t, ClearTestData())

    // Choose the appropriate shared table based on whether we need user index
    tableName := TEST_ACCOUNTS_TABLE
    if withUserIndex {
        tableName = TEST_ACCOUNTS_WITH_INDEX_TABLE
    }

    // Return store using shared table
    store := store_dynamodb.NewAccountDatabaseStore(GetTestClient(), tableName).(*store_dynamodb.AccountDatabaseStore)
    return GetTestClient(), store
}

func getEnv(key, fallback string) string {
    if value, ok := os.LookupEnv(key); ok {
        return value
    }
    return fallback
}

func GetClient() *dynamodb.Client {
    testDBUri := getEnv("DYNAMODB_URL", "http://localhost:8000")

    cfg, err := config.LoadDefaultConfig(context.TODO(),
        config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("dummy", "dummy_secret", "1234")),
        config.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc(
            func(service, region string, options ...interface{}) (aws.Endpoint, error) {
                return aws.Endpoint{URL: testDBUri}, nil
            })),
    )
    if err != nil {
        panic(err)
    }

    svc := dynamodb.NewFromConfig(cfg)
    globalTestClient = svc
    return svc
}

func TestInsertAndGetById(t *testing.T) {
    _, dynamo_store := setupStoreDynamoDBTest(t, false)
    id := uuid.New()
    registeredAccountId := id.String()
    store_account := store_dynamodb.Account{
        Uuid:        registeredAccountId,
        UserId:      "SomeId",
        AccountId:   "SomeAccountId",
        AccountType: "aws",
        RoleName:    "SomeRoleName",
        ExternalId:  "SomeExternalId",
    }
    err := dynamo_store.Insert(context.Background(), store_account)
    if err != nil {
        t.Errorf("error inserting item into table")
    }
    accountItem, err := dynamo_store.GetById(context.Background(), registeredAccountId)
    if err != nil {
        t.Errorf("error getting item from table")
    }

    if accountItem.Uuid != registeredAccountId {
        t.Errorf("expected uuid to equal %s", registeredAccountId)
    }

}

func TestInsertAndGetByUserId(t *testing.T) {
    _, dynamo_store := setupStoreDynamoDBTest(t, true)

    userId := "SomeUserId"
    uuids := []string{uuid.New().String(), uuid.New().String()}
    for _, u := range uuids {
        store_account := store_dynamodb.Account{
            Uuid:        u,
            UserId:      userId,
            AccountId:   u,
            AccountType: "aws",
            RoleName:    "SomeRoleName",
            ExternalId:  "SomeExternalId",
        }
        err := dynamo_store.Insert(context.Background(), store_account)
        if err != nil {
            t.Errorf("error inserting item into table: %v", err)
        }
    }

    // Test GetByUserId method
    accounts, err := dynamo_store.GetByUserId(context.Background(), userId)
    if err != nil {
        t.Errorf("error getting items by userId: %v", err)
    }

    if len(accounts) != len(uuids) {
        t.Errorf("expected %v accounts, not %v", len(uuids), len(accounts))
    }

    // Verify individual account
    account, err := dynamo_store.GetById(context.Background(), uuids[0])
    if err != nil {
        t.Errorf("error getting account by id: %v", err)
    }

    if account.Uuid != uuids[0] {
        t.Errorf("expected account uuid %v, got %v", uuids[0], account.Uuid)
    }

}

func CreateAccountsTable(dynamoDBClient *dynamodb.Client, tableName string) (*types.TableDescription, error) {
    var tableDesc *types.TableDescription
    table, err := dynamoDBClient.CreateTable(context.TODO(), &dynamodb.CreateTableInput{
        AttributeDefinitions: []types.AttributeDefinition{{
            AttributeName: aws.String("uuid"),
            AttributeType: types.ScalarAttributeTypeS,
        }},
        KeySchema: []types.KeySchemaElement{{
            AttributeName: aws.String("uuid"),
            KeyType:       types.KeyTypeHash,
        }},
        TableName:   aws.String(tableName),
        BillingMode: types.BillingModePayPerRequest,
    })
    if err != nil {
        log.Printf("couldn't create table %v. Here's why: %v\n", tableName, err)
    } else {
        waiter := dynamodb.NewTableExistsWaiter(dynamoDBClient)
        err = waiter.Wait(context.TODO(), &dynamodb.DescribeTableInput{
            TableName: aws.String(tableName)}, 5*time.Minute)
        if err != nil {
            log.Printf("wait for table exists failed. Here's why: %v\n", err)
        }
        tableDesc = table.TableDescription
    }
    return tableDesc, err
}

func CreateAccountsTableWithUserIndex(dynamoDBClient *dynamodb.Client, tableName string) (*types.TableDescription, error) {
    var tableDesc *types.TableDescription
    table, err := dynamoDBClient.CreateTable(context.TODO(), &dynamodb.CreateTableInput{
        AttributeDefinitions: []types.AttributeDefinition{
            {
                AttributeName: aws.String("uuid"),
                AttributeType: types.ScalarAttributeTypeS,
            },
            {
                AttributeName: aws.String("userId"),
                AttributeType: types.ScalarAttributeTypeS,
            },
        },
        KeySchema: []types.KeySchemaElement{{
            AttributeName: aws.String("uuid"),
            KeyType:       types.KeyTypeHash,
        }},
        GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
            {
                IndexName: aws.String("userId-index"),
                KeySchema: []types.KeySchemaElement{
                    {
                        AttributeName: aws.String("userId"),
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
        log.Printf("couldn't create table %v. Here's why: %v\n", tableName, err)
    } else {
        waiter := dynamodb.NewTableExistsWaiter(dynamoDBClient)
        err = waiter.Wait(context.TODO(), &dynamodb.DescribeTableInput{
            TableName: aws.String(tableName)}, 5*time.Minute)
        if err != nil {
            log.Printf("wait for table exists failed. Here's why: %v\n", err)
        }
        tableDesc = table.TableDescription
    }
    return tableDesc, err
}

func DeleteTable(dynamoDBClient *dynamodb.Client, tableName string) error {
    _, err := dynamoDBClient.DeleteTable(context.TODO(), &dynamodb.DeleteTableInput{
        TableName: aws.String(tableName)})
    if err != nil {
        log.Printf("couldn't delete table %v. Here's why: %v\n", tableName, err)
    }
    return err
}

// tableExists checks if a table exists
func tableExists(client *dynamodb.Client, tableName string) (bool, error) {
    _, err := client.DescribeTable(context.TODO(), &dynamodb.DescribeTableInput{
        TableName: aws.String(tableName),
    })
    if err != nil {
        // Check if error is ResourceNotFoundException
        if strings.Contains(err.Error(), "ResourceNotFoundException") {
            return false, nil
        }
        return false, err
    }
    return true, nil
}

// GenerateTestId generates a unique test ID for test isolation
func GenerateTestId() string {
    // Use a short UUID suffix for readability
    id := uuid.New().String()
    return id[len(id)-8:]
}

// ensureTableFreshState ensures table is in clean state - either creates new or clears existing
func ensureTableFreshState(client *dynamodb.Client, tableName string, createFunc func() error) error {
    exists, err := tableExists(client, tableName)
    if err != nil {
        return err
    }

    if exists {
        // Table exists - just clear its data (much faster than delete/recreate)
        if err := ClearStoreDynamoDBTable(client, tableName); err != nil {
            // If clearing fails, fall back to delete and recreate
            log.Printf("Failed to clear table %s, will recreate: %v", tableName, err)
            _ = DeleteTable(client, tableName)
            // Wait for deletion to complete
            waiter := dynamodb.NewTableNotExistsWaiter(client)
            _ = waiter.Wait(context.TODO(), &dynamodb.DescribeTableInput{
                TableName: aws.String(tableName),
            }, 30*time.Second)
            // Create new table
            if err := createFunc(); err != nil {
                return err
            }
            log.Printf("Recreated table: %s", tableName)
        } else {
            log.Printf("Cleared existing table: %s", tableName)
        }
    } else {
        // Table doesn't exist - create it
        if err := createFunc(); err != nil {
            return err
        }
        log.Printf("Created table: %s", tableName)
    }

    return nil
}
