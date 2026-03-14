package store_dynamodb

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/account-service/internal/models"
)

type HealthCheckLogStore interface {
	PutLog(context.Context, models.DynamoDBHealthCheckLog) error
}

type HealthCheckLogDatabaseStore struct {
	DB        *dynamodb.Client
	TableName string
}

func NewHealthCheckLogDatabaseStore(db *dynamodb.Client, tableName string) HealthCheckLogStore {
	return &HealthCheckLogDatabaseStore{db, tableName}
}

func (r *HealthCheckLogDatabaseStore) PutLog(ctx context.Context, logEntry models.DynamoDBHealthCheckLog) error {
	item, err := attributevalue.MarshalMap(logEntry)
	if err != nil {
		return fmt.Errorf("error marshaling health check log: %w", err)
	}

	_, err = r.DB.PutItem(ctx, &dynamodb.PutItemInput{
		Item:      item,
		TableName: aws.String(r.TableName),
	})
	if err != nil {
		return fmt.Errorf("error putting health check log: %w", err)
	}

	return nil
}