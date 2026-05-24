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

type ChatUserQuotaStore interface {
	Get(ctx context.Context, computeNodeId, userId string) (models.ChatUserQuota, error)
	Put(ctx context.Context, quota models.ChatUserQuota) error
	Delete(ctx context.Context, computeNodeId, userId string) error
	ListByNode(ctx context.Context, computeNodeId string) ([]models.ChatUserQuota, error)
}

type ChatUserQuotaDatabaseStore struct {
	DB        *dynamodb.Client
	TableName string
}

func NewChatUserQuotaStore(db *dynamodb.Client, tableName string) ChatUserQuotaStore {
	return &ChatUserQuotaDatabaseStore{db, tableName}
}

// Get returns the quota row for (computeNodeId, userId). If no row exists, an
// empty ChatUserQuota (with empty ComputeNodeId) is returned and err is nil —
// callers should treat that as "no override; fall back to default".
func (r *ChatUserQuotaDatabaseStore) Get(ctx context.Context, computeNodeId, userId string) (models.ChatUserQuota, error) {
	key := models.ChatUserQuota{ComputeNodeId: computeNodeId, UserId: userId}
	response, err := r.DB.GetItem(ctx, &dynamodb.GetItemInput{
		Key:       key.GetKey(),
		TableName: aws.String(r.TableName),
	})
	if err != nil {
		return models.ChatUserQuota{}, fmt.Errorf("error getting chat user quota: %w", err)
	}
	if response.Item == nil {
		return models.ChatUserQuota{}, nil
	}

	var out models.ChatUserQuota
	if err := attributevalue.UnmarshalMap(response.Item, &out); err != nil {
		return models.ChatUserQuota{}, fmt.Errorf("error unmarshaling chat user quota: %w", err)
	}
	return out, nil
}

func (r *ChatUserQuotaDatabaseStore) Put(ctx context.Context, quota models.ChatUserQuota) error {
	item, err := attributevalue.MarshalMap(quota)
	if err != nil {
		return fmt.Errorf("error marshaling chat user quota: %w", err)
	}

	_, err = r.DB.PutItem(ctx, &dynamodb.PutItemInput{
		Item:      item,
		TableName: aws.String(r.TableName),
	})
	if err != nil {
		return fmt.Errorf("error putting chat user quota: %w", err)
	}
	return nil
}

func (r *ChatUserQuotaDatabaseStore) Delete(ctx context.Context, computeNodeId, userId string) error {
	key := models.ChatUserQuota{ComputeNodeId: computeNodeId, UserId: userId}
	_, err := r.DB.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		Key:       key.GetKey(),
		TableName: aws.String(r.TableName),
	})
	if err != nil {
		return fmt.Errorf("error deleting chat user quota: %w", err)
	}
	return nil
}

func (r *ChatUserQuotaDatabaseStore) ListByNode(ctx context.Context, computeNodeId string) ([]models.ChatUserQuota, error) {
	quotas := []models.ChatUserQuota{}

	keyCond := expression.Key("computeNodeId").Equal(expression.Value(computeNodeId))
	expr, err := expression.NewBuilder().WithKeyCondition(keyCond).Build()
	if err != nil {
		return quotas, fmt.Errorf("error building expression: %w", err)
	}

	response, err := r.DB.Query(ctx, &dynamodb.QueryInput{
		KeyConditionExpression:    expr.KeyCondition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		TableName:                 aws.String(r.TableName),
	})
	if err != nil {
		return quotas, fmt.Errorf("error querying chat user quotas: %w", err)
	}

	if err := attributevalue.UnmarshalListOfMaps(response.Items, &quotas); err != nil {
		return quotas, fmt.Errorf("error unmarshaling chat user quotas: %w", err)
	}
	return quotas, nil
}
