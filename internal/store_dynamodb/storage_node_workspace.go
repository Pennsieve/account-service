package store_dynamodb

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/account-service/internal/models"
)

type StorageNodeWorkspaceStore interface {
	Insert(context.Context, models.DynamoDBStorageNodeWorkspace) error
	Delete(context.Context, string, string) error
	GetByStorageNode(context.Context, string) ([]models.DynamoDBStorageNodeWorkspace, error)
	GetByWorkspace(context.Context, string) ([]models.DynamoDBStorageNodeWorkspace, error)
	Get(context.Context, string, string) (models.DynamoDBStorageNodeWorkspace, error)
}

type StorageNodeWorkspaceStoreImpl struct {
	DB        *dynamodb.Client
	TableName string
}

func NewStorageNodeWorkspaceStore(db *dynamodb.Client, tableName string) StorageNodeWorkspaceStore {
	return &StorageNodeWorkspaceStoreImpl{db, tableName}
}

func (r *StorageNodeWorkspaceStoreImpl) Insert(ctx context.Context, enablement models.DynamoDBStorageNodeWorkspace) error {
	item, err := attributevalue.MarshalMap(enablement)
	if err != nil {
		return fmt.Errorf("error marshaling storage node workspace enablement: %w", err)
	}
	_, err = r.DB.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(r.TableName), Item: item,
	})
	if err != nil {
		return fmt.Errorf("error inserting storage node workspace enablement: %w", err)
	}

	return nil
}

func (r *StorageNodeWorkspaceStoreImpl) Delete(ctx context.Context, storageNodeUuid string, workspaceId string) error {
	enablement := models.DynamoDBStorageNodeWorkspace{
		StorageNodeUuid: storageNodeUuid,
		WorkspaceId:     workspaceId,
	}
	_, err := r.DB.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		Key:       enablement.GetKey(),
		TableName: aws.String(r.TableName),
	})
	if err != nil {
		return fmt.Errorf("error deleting storage node workspace enablement: %w", err)
	}
	return nil
}

func (r *StorageNodeWorkspaceStoreImpl) GetByStorageNode(ctx context.Context, storageNodeUuid string) ([]models.DynamoDBStorageNodeWorkspace, error) {
	var enablements []models.DynamoDBStorageNodeWorkspace

	expr, err := expression.NewBuilder().WithKeyCondition(
		expression.Key("storageNodeUuid").Equal(expression.Value(storageNodeUuid)),
	).Build()
	if err != nil {
		return enablements, fmt.Errorf("error building expression: %w", err)
	}

	response, err := r.DB.Query(ctx, &dynamodb.QueryInput{
		TableName:                 aws.String(r.TableName),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		KeyConditionExpression:    expr.KeyCondition(),
	})
	if err != nil {
		return enablements, fmt.Errorf("error querying storage node workspace enablements: %w", err)
	}

	err = attributevalue.UnmarshalListOfMaps(response.Items, &enablements)
	if err != nil {
		return enablements, fmt.Errorf("error unmarshaling storage node workspace enablements: %w", err)
	}

	return enablements, nil
}

func (r *StorageNodeWorkspaceStoreImpl) GetByWorkspace(ctx context.Context, workspaceId string) ([]models.DynamoDBStorageNodeWorkspace, error) {
	var enablements []models.DynamoDBStorageNodeWorkspace

	expr, err := expression.NewBuilder().WithKeyCondition(
		expression.Key("workspaceId").Equal(expression.Value(workspaceId)),
	).Build()
	if err != nil {
		return enablements, fmt.Errorf("error building expression: %w", err)
	}

	response, err := r.DB.Query(ctx, &dynamodb.QueryInput{
		TableName:                 aws.String(r.TableName),
		IndexName:                 aws.String("workspaceId-index"),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		KeyConditionExpression:    expr.KeyCondition(),
	})
	if err != nil {
		return enablements, fmt.Errorf("error querying storage node workspace enablements: %w", err)
	}

	err = attributevalue.UnmarshalListOfMaps(response.Items, &enablements)
	if err != nil {
		return enablements, fmt.Errorf("error unmarshaling storage node workspace enablements: %w", err)
	}

	return enablements, nil
}

func (r *StorageNodeWorkspaceStoreImpl) Get(ctx context.Context, storageNodeUuid string, workspaceId string) (models.DynamoDBStorageNodeWorkspace, error) {
	enablement := models.DynamoDBStorageNodeWorkspace{
		StorageNodeUuid: storageNodeUuid,
		WorkspaceId:     workspaceId,
	}

	response, err := r.DB.GetItem(ctx, &dynamodb.GetItemInput{
		Key:       enablement.GetKey(),
		TableName: aws.String(r.TableName),
	})
	if err != nil {
		return models.DynamoDBStorageNodeWorkspace{}, fmt.Errorf("error getting storage node workspace enablement: %w", err)
	}
	if response.Item == nil {
		return models.DynamoDBStorageNodeWorkspace{}, nil
	}

	err = attributevalue.UnmarshalMap(response.Item, &enablement)
	if err != nil {
		return enablement, fmt.Errorf("error unmarshaling storage node workspace enablement: %w", err)
	}

	return enablement, nil
}
