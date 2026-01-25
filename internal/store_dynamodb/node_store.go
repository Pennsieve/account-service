package store_dynamodb

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/pennsieve/account-service/internal/models"
)

type NodeStore interface {
	GetById(context.Context, string) (models.DynamoDBNode, error)
	Get(context.Context, string) ([]models.DynamoDBNode, error)
	Put(context.Context, models.DynamoDBNode) error
	Delete(context.Context, string) error
}

type NodeDatabaseStore struct {
	DB        *dynamodb.Client
	TableName string
}

func NewNodeDatabaseStore(db *dynamodb.Client, tableName string) NodeStore {
	return &NodeDatabaseStore{db, tableName}
}

func (r *NodeDatabaseStore) GetById(ctx context.Context, uuid string) (models.DynamoDBNode, error) {
	node := models.DynamoDBNode{Uuid: uuid}
	response, err := r.DB.GetItem(ctx, &dynamodb.GetItemInput{
		Key: node.GetKey(), 
		TableName: aws.String(r.TableName),
	})
	if err != nil {
		return models.DynamoDBNode{}, fmt.Errorf("error getting node: %w", err)
	}
	if response.Item == nil {
		return models.DynamoDBNode{}, nil
	}

	err = attributevalue.UnmarshalMap(response.Item, &node)
	if err != nil {
		return node, fmt.Errorf("error unmarshaling node: %w", err)
	}

	return node, nil
}

func (r *NodeDatabaseStore) Get(ctx context.Context, filter string) ([]models.DynamoDBNode, error) {
	nodes := []models.DynamoDBNode{}
	filt := expression.Name("organizationId").Equal((expression.Value(filter)))
	expr, err := expression.NewBuilder().WithFilter(filt).Build()
	if err != nil {
		return nodes, fmt.Errorf("error building expression: %w", err)
	}

	response, err := r.DB.Scan(ctx, &dynamodb.ScanInput{
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		FilterExpression:          expr.Filter(),
		ProjectionExpression:      expr.Projection(),
		TableName:                 aws.String(r.TableName),
	})
	if err != nil {
		return nodes, fmt.Errorf("error getting nodes: %w", err)
	}

	err = attributevalue.UnmarshalListOfMaps(response.Items, &nodes)
	if err != nil {
		return nodes, fmt.Errorf("error unmarshaling nodes: %w", err)
	}

	return nodes, nil
}

func (r *NodeDatabaseStore) Put(ctx context.Context, node models.DynamoDBNode) error {
	item, err := attributevalue.MarshalMap(node)
	if err != nil {
		return fmt.Errorf("error marshaling node: %w", err)
	}

	_, err = r.DB.PutItem(ctx, &dynamodb.PutItemInput{
		Item:      item,
		TableName: aws.String(r.TableName),
	})
	if err != nil {
		return fmt.Errorf("error putting node: %w", err)
	}

	return nil
}

func (r *NodeDatabaseStore) Delete(ctx context.Context, uuid string) error {
	node := models.DynamoDBNode{Uuid: uuid}
	_, err := r.DB.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		Key:       node.GetKey(),
		TableName: aws.String(r.TableName),
	})
	if err != nil {
		return fmt.Errorf("error deleting node: %w", err)
	}

	return nil
}