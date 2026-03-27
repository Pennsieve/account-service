package store_dynamodb

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/pennsieve/account-service/internal/models"
)

type StorageNodeStore interface {
	GetById(context.Context, string) (models.DynamoDBStorageNode, error)
	GetByAccount(context.Context, string) ([]models.DynamoDBStorageNode, error)
	GetAllEnabled(context.Context) ([]models.DynamoDBStorageNode, error)
	Put(context.Context, models.DynamoDBStorageNode) error
	Delete(context.Context, string) error
}

type StorageNodeDatabaseStore struct {
	DB        *dynamodb.Client
	TableName string
}

func NewStorageNodeDatabaseStore(db *dynamodb.Client, tableName string) StorageNodeStore {
	return &StorageNodeDatabaseStore{db, tableName}
}

func (r *StorageNodeDatabaseStore) GetById(ctx context.Context, uuid string) (models.DynamoDBStorageNode, error) {
	node := models.DynamoDBStorageNode{Uuid: uuid}
	response, err := r.DB.GetItem(ctx, &dynamodb.GetItemInput{
		Key:       node.GetKey(),
		TableName: aws.String(r.TableName),
	})
	if err != nil {
		return models.DynamoDBStorageNode{}, fmt.Errorf("error getting storage node: %w", err)
	}
	if response.Item == nil {
		return models.DynamoDBStorageNode{}, nil
	}

	err = attributevalue.UnmarshalMap(response.Item, &node)
	if err != nil {
		return node, fmt.Errorf("error unmarshaling storage node: %w", err)
	}

	return node, nil
}

func (r *StorageNodeDatabaseStore) GetByAccount(ctx context.Context, accountUuid string) ([]models.DynamoDBStorageNode, error) {
	var nodes []models.DynamoDBStorageNode
	keyCond := expression.Key("accountUuid").Equal(expression.Value(accountUuid))
	expr, err := expression.NewBuilder().WithKeyCondition(keyCond).Build()
	if err != nil {
		return nodes, fmt.Errorf("error building expression: %w", err)
	}

	response, err := r.DB.Query(ctx, &dynamodb.QueryInput{
		IndexName:                 aws.String("accountUuid-index"),
		KeyConditionExpression:    expr.KeyCondition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		TableName:                 aws.String(r.TableName),
	})
	if err != nil {
		return nodes, fmt.Errorf("error getting storage nodes by account: %w", err)
	}

	err = attributevalue.UnmarshalListOfMaps(response.Items, &nodes)
	if err != nil {
		return nodes, fmt.Errorf("error unmarshaling storage nodes: %w", err)
	}

	return nodes, nil
}

func (r *StorageNodeDatabaseStore) GetAllEnabled(ctx context.Context) ([]models.DynamoDBStorageNode, error) {
	var nodes []models.DynamoDBStorageNode
	filt := expression.Name("status").Equal(expression.Value("Enabled"))
	expr, err := expression.NewBuilder().WithFilter(filt).Build()
	if err != nil {
		return nodes, fmt.Errorf("error building expression: %w", err)
	}

	var lastKey map[string]ddbtypes.AttributeValue
	for {
		input := &dynamodb.ScanInput{
			ExpressionAttributeNames:  expr.Names(),
			ExpressionAttributeValues: expr.Values(),
			FilterExpression:          expr.Filter(),
			TableName:                 aws.String(r.TableName),
			ExclusiveStartKey:         lastKey,
		}

		response, err := r.DB.Scan(ctx, input)
		if err != nil {
			return nodes, fmt.Errorf("error scanning enabled storage nodes: %w", err)
		}

		var page []models.DynamoDBStorageNode
		err = attributevalue.UnmarshalListOfMaps(response.Items, &page)
		if err != nil {
			return nodes, fmt.Errorf("error unmarshaling storage nodes: %w", err)
		}
		nodes = append(nodes, page...)

		if response.LastEvaluatedKey == nil {
			break
		}
		lastKey = response.LastEvaluatedKey
	}

	return nodes, nil
}

func (r *StorageNodeDatabaseStore) Put(ctx context.Context, node models.DynamoDBStorageNode) error {
	item, err := attributevalue.MarshalMap(node)
	if err != nil {
		return fmt.Errorf("error marshaling storage node: %w", err)
	}

	_, err = r.DB.PutItem(ctx, &dynamodb.PutItemInput{
		Item:      item,
		TableName: aws.String(r.TableName),
	})
	if err != nil {
		return fmt.Errorf("error putting storage node: %w", err)
	}

	return nil
}

func (r *StorageNodeDatabaseStore) Delete(ctx context.Context, uuid string) error {
	node := models.DynamoDBStorageNode{Uuid: uuid}
	_, err := r.DB.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		Key:       node.GetKey(),
		TableName: aws.String(r.TableName),
	})
	if err != nil {
		return fmt.Errorf("error deleting storage node: %w", err)
	}

	return nil
}
