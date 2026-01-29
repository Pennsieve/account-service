package store_dynamodb_test

import (
    "context"
    "github.com/pennsieve/account-service/internal/test"
    "strings"
    "testing"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
    "github.com/google/uuid"
    "github.com/pennsieve/account-service/internal/models"
    "github.com/pennsieve/account-service/internal/store_dynamodb"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func clearNodesTable(client *dynamodb.Client, tableName string) error {
    // Scan the table to get all items
    scanResult, err := client.Scan(context.TODO(), &dynamodb.ScanInput{
        TableName: aws.String(tableName),
    })
    if err != nil {
        return err
    }

    // Delete all items
    for _, item := range scanResult.Items {
        uuid := item["uuid"]

        _, err = client.DeleteItem(context.TODO(), &dynamodb.DeleteItemInput{
            TableName: aws.String(tableName),
            Key: map[string]types.AttributeValue{
                "uuid": uuid,
            },
        })
        if err != nil {
            return err
        }
    }

    return nil
}

func isNodeTableExistsError(err error) bool {
    return err.Error() != "" && (
        err.Error() == "ResourceInUseException: Cannot create preexisting table" ||
            strings.Contains(err.Error(), "ResourceInUseException") ||
            strings.Contains(err.Error(), "preexisting table"))
}

func setupNodeStoreTest(t *testing.T) store_dynamodb.NodeStore {
    // Clear data from previous tests
    // Don't clear all test data - use unique IDs to avoid conflicts with other tests

    // Return store using shared table
    store := store_dynamodb.NewNodeDatabaseStore(test.GetTestClient(), TEST_NODES_TABLE)
    return store
}

func CreateNodesTable(dynamoDBClient *dynamodb.Client, tableName string) (*types.TableDescription, error) {
    var tableDesc *types.TableDescription
    table, err := dynamoDBClient.CreateTable(context.TODO(), &dynamodb.CreateTableInput{
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
        BillingMode: types.BillingModePayPerRequest,
        TableName:   aws.String(tableName),
    })

    if err != nil {
        return tableDesc, err
    } else {
        tableDesc = table.TableDescription
    }

    return tableDesc, err
}

func TestNodeStore_Put(t *testing.T) {
    store := setupNodeStoreTest(t)
    nodeUuid := uuid.New().String()

    node := models.DynamoDBNode{
        Uuid:                  nodeUuid,
        Name:                  "test-node",
        Description:           "Test compute node",
        ComputeNodeGatewayUrl: "https://test-gateway.example.com",
        EfsId:                 "fs-12345",
        QueueUrl:              "https://sqs.region.amazonaws.com/123/queue",
        Env:                   "test",
        AccountUuid:           "account-uuid-123",
        AccountId:             "123456789012",
        AccountType:           "aws",
        CreatedAt:             "2024-01-25T10:00:00Z",
        OrganizationId:        "org-123",
        UserId:                "user-456",
        Identifier:            "node-identifier-789",
        WorkflowManagerTag:    "v1.0.0",
    }

    // Test Put
    err := store.Put(context.Background(), node)
    assert.NoError(t, err, "Put should not return error")

    // Verify the node was stored
    retrievedNode, err := store.GetById(context.Background(), nodeUuid)
    require.NoError(t, err, "GetById should not return error")

    assert.Equal(t, node.Uuid, retrievedNode.Uuid)
    assert.Equal(t, node.Name, retrievedNode.Name)
    assert.Equal(t, node.Description, retrievedNode.Description)
    assert.Equal(t, node.ComputeNodeGatewayUrl, retrievedNode.ComputeNodeGatewayUrl)
    assert.Equal(t, node.EfsId, retrievedNode.EfsId)
    assert.Equal(t, node.QueueUrl, retrievedNode.QueueUrl)
    assert.Equal(t, node.Env, retrievedNode.Env)
    assert.Equal(t, node.AccountUuid, retrievedNode.AccountUuid)
    assert.Equal(t, node.AccountId, retrievedNode.AccountId)
    assert.Equal(t, node.AccountType, retrievedNode.AccountType)
    assert.Equal(t, node.CreatedAt, retrievedNode.CreatedAt)
    assert.Equal(t, node.OrganizationId, retrievedNode.OrganizationId)
    assert.Equal(t, node.UserId, retrievedNode.UserId)
    assert.Equal(t, node.Identifier, retrievedNode.Identifier)
    assert.Equal(t, node.WorkflowManagerTag, retrievedNode.WorkflowManagerTag)
}

func TestNodeStore_GetById_NotFound(t *testing.T) {
    store := setupNodeStoreTest(t)
    nonExistentId := uuid.New().String()

    // Test GetById with non-existent ID
    node, err := store.GetById(context.Background(), nonExistentId)
    assert.NoError(t, err, "GetById should not return error for non-existent item")
    assert.Empty(t, node.Uuid, "Node should be empty for non-existent item")
}

func TestNodeStore_Get_FilterByOrganization(t *testing.T) {
    store := setupNodeStoreTest(t)
    testId := test.GenerateTestId()
    orgId := "org-123-" + testId
    otherOrgId := "org-456-" + testId

    // Insert nodes for the test organization
    node1 := models.DynamoDBNode{
        Uuid:           uuid.New().String(),
        Name:           "node-1",
        Description:    "First test node",
        OrganizationId: orgId,
        UserId:         "user-123-" + testId,
        AccountUuid:    "account-1-" + testId,
        AccountId:      "account-1-" + testId,
        AccountType:    "aws",
        CreatedAt:      "2024-01-25T10:00:00Z",
    }
    node2 := models.DynamoDBNode{
        Uuid:           uuid.New().String(),
        Name:           "node-2",
        Description:    "Second test node",
        OrganizationId: orgId,
        UserId:         "user-123-" + testId,
        AccountUuid:    "account-2-" + testId,
        AccountId:      "account-2-" + testId,
        AccountType:    "gcp",
        CreatedAt:      "2024-01-25T11:00:00Z",
    }

    // Insert node for different organization
    otherNode := models.DynamoDBNode{
        Uuid:           uuid.New().String(),
        Name:           "other-node",
        Description:    "Node in different org",
        OrganizationId: otherOrgId,
        UserId:         "user-456-" + testId,
        AccountUuid:    "account-3-" + testId,
        AccountId:      "account-3-" + testId,
        AccountType:    "aws",
        CreatedAt:      "2024-01-25T12:00:00Z",
    }

    err := store.Put(context.Background(), node1)
    require.NoError(t, err, "Failed to put node1")
    err = store.Put(context.Background(), node2)
    require.NoError(t, err, "Failed to put node2")
    err = store.Put(context.Background(), otherNode)
    require.NoError(t, err, "Failed to put otherNode")

    // Test Get with organization filter
    nodes, err := store.Get(context.Background(), orgId)
    assert.NoError(t, err, "Get should not return error")

    // Should return only nodes for the specified organization
    assert.Len(t, nodes, 2, "Should return 2 nodes for the organization")

    for _, node := range nodes {
        assert.Equal(t, orgId, node.OrganizationId, "All returned nodes should belong to the specified organization")
    }
}

func TestNodeStore_Delete(t *testing.T) {
    store := setupNodeStoreTest(t)
    nodeUuid := uuid.New().String()

    // Insert node
    node := models.DynamoDBNode{
        Uuid:           nodeUuid,
        Name:           "test-node-to-delete",
        Description:    "Node that will be deleted",
        OrganizationId: "org-123",
        UserId:         "user-123",
        AccountUuid:    "account-123",
        AccountId:      "account-123",
        AccountType:    "aws",
        CreatedAt:      "2024-01-25T10:00:00Z",
    }

    err := store.Put(context.Background(), node)
    require.NoError(t, err, "Failed to put node")

    // Verify node exists
    retrievedNode, err := store.GetById(context.Background(), nodeUuid)
    require.NoError(t, err, "GetById should not return error")
    assert.Equal(t, nodeUuid, retrievedNode.Uuid, "Node should exist before deletion")

    // Test Delete
    err = store.Delete(context.Background(), nodeUuid)
    assert.NoError(t, err, "Delete should not return error")

    // Verify node is deleted
    deletedNode, err := store.GetById(context.Background(), nodeUuid)
    assert.NoError(t, err, "GetById should not return error after deletion")
    assert.Empty(t, deletedNode.Uuid, "Node should be empty after deletion")
}

func TestNodeStore_Delete_NonExistent(t *testing.T) {
    store := setupNodeStoreTest(t)
    nonExistentId := uuid.New().String()

    // Test Delete with non-existent ID
    err := store.Delete(context.Background(), nonExistentId)
    assert.NoError(t, err, "Delete should not return error for non-existent item")
}

func TestNodeStore_Put_Update(t *testing.T) {
    store := setupNodeStoreTest(t)
    nodeUuid := uuid.New().String()

    // Insert initial node
    originalNode := models.DynamoDBNode{
        Uuid:                  nodeUuid,
        Name:                  "original-node",
        Description:           "Original description",
        ComputeNodeGatewayUrl: "https://original-gateway.example.com",
        WorkflowManagerTag:    "v1.0.0",
        OrganizationId:        "org-123",
        UserId:                "user-123",
        AccountUuid:           "account-123",
        AccountId:             "account-123",
        AccountType:           "aws",
        CreatedAt:             "2024-01-25T10:00:00Z",
    }

    err := store.Put(context.Background(), originalNode)
    require.NoError(t, err, "Failed to put original node")

    // Update node using Put (upsert behavior)
    updatedNode := originalNode
    updatedNode.Name = "updated-node"
    updatedNode.Description = "Updated description"
    updatedNode.ComputeNodeGatewayUrl = "https://updated-gateway.example.com"
    updatedNode.WorkflowManagerTag = "v2.0.0"

    err = store.Put(context.Background(), updatedNode)
    assert.NoError(t, err, "Put update should not return error")

    // Verify the node was updated
    retrievedNode, err := store.GetById(context.Background(), nodeUuid)
    require.NoError(t, err, "GetById should not return error")

    assert.Equal(t, updatedNode.Uuid, retrievedNode.Uuid)
    assert.Equal(t, updatedNode.Name, retrievedNode.Name)
    assert.Equal(t, updatedNode.Description, retrievedNode.Description)
    assert.Equal(t, updatedNode.ComputeNodeGatewayUrl, retrievedNode.ComputeNodeGatewayUrl)
    assert.Equal(t, updatedNode.WorkflowManagerTag, retrievedNode.WorkflowManagerTag)
    assert.Equal(t, updatedNode.OrganizationId, retrievedNode.OrganizationId)
    assert.Equal(t, updatedNode.UserId, retrievedNode.UserId)
}
