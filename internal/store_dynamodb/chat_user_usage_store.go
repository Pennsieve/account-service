package store_dynamodb

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pennsieve/account-service/internal/models"
)

// defaultUsageRetention is how long aggregate rows live in DynamoDB before
// the TTL sweeps them. Mirrors the governor's 90-day retention.
const defaultUsageRetention = 90 * 24 * time.Hour

// UsageIncrement carries the delta to apply on a single accounting write.
type UsageIncrement struct {
	UserId           string
	ComputeNodeId    string
	InputTokens      int64
	OutputTokens     int64
	EstimatedCostUsd float64
	Model            string
}

type ChatUserUsageStore interface {
	Get(ctx context.Context, userNodeDate string) (models.ChatUserUsage, error)
	Increment(ctx context.Context, inc UsageIncrement, now time.Time) error
}

type ChatUserUsageDatabaseStore struct {
	DB        *dynamodb.Client
	TableName string
}

func NewChatUserUsageStore(db *dynamodb.Client, tableName string) ChatUserUsageStore {
	return &ChatUserUsageDatabaseStore{db, tableName}
}

// Get returns the aggregate row identified by userNodeDate (which may be a
// daily or monthly key). Returns an empty struct with no error if no row exists.
func (r *ChatUserUsageDatabaseStore) Get(ctx context.Context, userNodeDate string) (models.ChatUserUsage, error) {
	key := models.ChatUserUsage{UserNodeDate: userNodeDate, RecordType: models.AggregateRecordType}
	response, err := r.DB.GetItem(ctx, &dynamodb.GetItemInput{
		Key:       key.GetKey(),
		TableName: aws.String(r.TableName),
	})
	if err != nil {
		return models.ChatUserUsage{}, fmt.Errorf("error getting chat user usage: %w", err)
	}
	if response.Item == nil {
		return models.ChatUserUsage{}, nil
	}

	var out models.ChatUserUsage
	if err := attributevalue.UnmarshalMap(response.Item, &out); err != nil {
		return models.ChatUserUsage{}, fmt.Errorf("error unmarshaling chat user usage: %w", err)
	}
	return out, nil
}

// Increment atomically adds the increment to BOTH the daily and monthly
// aggregate rows for (userId, computeNodeId) at the moment 'now'. The rows
// are created if they don't exist.
func (r *ChatUserUsageDatabaseStore) Increment(ctx context.Context, inc UsageIncrement, now time.Time) error {
	dayKey := models.BuildDailyUsageKey(inc.UserId, inc.ComputeNodeId, now.UTC().Format("2006-01-02"))
	monthKey := models.BuildMonthlyUsageKey(inc.UserId, inc.ComputeNodeId, now.UTC().Format("2006-01"))
	expiresAt := now.Add(defaultUsageRetention).Unix()
	updatedAt := now.UTC().Format(time.RFC3339)

	if err := r.applyIncrement(ctx, dayKey, "day", inc, updatedAt, expiresAt); err != nil {
		return fmt.Errorf("daily increment: %w", err)
	}
	if err := r.applyIncrement(ctx, monthKey, "month", inc, updatedAt, expiresAt); err != nil {
		return fmt.Errorf("monthly increment: %w", err)
	}
	return nil
}

func (r *ChatUserUsageDatabaseStore) applyIncrement(ctx context.Context, hashKey, period string, inc UsageIncrement, updatedAt string, expiresAt int64) error {
	key := models.ChatUserUsage{UserNodeDate: hashKey, RecordType: models.AggregateRecordType}

	update := expression.
		Add(expression.Name("totalInputTokens"), expression.Value(inc.InputTokens)).
		Add(expression.Name("totalOutputTokens"), expression.Value(inc.OutputTokens)).
		Add(expression.Name("estimatedCostUsd"), expression.Value(inc.EstimatedCostUsd)).
		Add(expression.Name("requestCount"), expression.Value(int64(1))).
		Set(expression.Name("userId"), expression.Value(inc.UserId)).
		Set(expression.Name("computeNodeId"), expression.Value(inc.ComputeNodeId)).
		Set(expression.Name("period"), expression.Value(period)).
		Set(expression.Name("updatedAt"), expression.Value(updatedAt)).
		Set(expression.Name("expiresAt"), expression.Value(expiresAt))

	if inc.Model != "" {
		update = update.Set(expression.Name("lastModel"), expression.Value(inc.Model))
	}

	expr, err := expression.NewBuilder().WithUpdate(update).Build()
	if err != nil {
		return fmt.Errorf("error building update expression: %w", err)
	}

	_, err = r.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		Key:                       key.GetKey(),
		TableName:                 aws.String(r.TableName),
		UpdateExpression:          expr.Update(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	if err != nil {
		return fmt.Errorf("error updating chat user usage: %w", err)
	}
	return nil
}
