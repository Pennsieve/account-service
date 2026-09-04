package store_dynamodb

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/pennsieve/account-service/internal/models"
)

// NodeQuotaStore is the pipeline spend policy for compute nodes. See
// models.NodeQuota for the key layout and why this is separate from the
// chat/LLM quota table.
type NodeQuotaStore interface {
	// Put writes one policy row, replacing whatever was there.
	Put(ctx context.Context, quota models.NodeQuota) error
	// Get reads one policy row by sort key. A missing row comes back as the
	// zero value with no error — absent policy is a valid state (unlimited).
	Get(ctx context.Context, nodeUuid, sk string) (models.NodeQuota, error)
	// ListForNode returns every policy row on the node in one query, which is
	// all three resolution tiers plus any other users' overrides.
	ListForNode(ctx context.Context, nodeUuid string) ([]models.NodeQuota, error)
	// Delete removes one policy row. Deleting an absent row is not an error.
	Delete(ctx context.Context, nodeUuid, sk string) error
}

type NodeQuotaDatabaseStore struct {
	DB        *dynamodb.Client
	TableName string
}

func NewNodeQuotaStore(db *dynamodb.Client, tableName string) NodeQuotaStore {
	return &NodeQuotaDatabaseStore{DB: db, TableName: tableName}
}

func (s *NodeQuotaDatabaseStore) Put(ctx context.Context, quota models.NodeQuota) error {
	if quota.PK == "" || quota.SK == "" {
		return fmt.Errorf("node quota row requires pk and sk")
	}

	item, err := attributevalue.MarshalMap(quota)
	if err != nil {
		return fmt.Errorf("marshalling node quota %s/%s: %w", quota.PK, quota.SK, err)
	}

	if _, err := s.DB.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.TableName),
		Item:      item,
	}); err != nil {
		return fmt.Errorf("putting node quota %s/%s: %w", quota.PK, quota.SK, err)
	}
	return nil
}

func (s *NodeQuotaDatabaseStore) Get(ctx context.Context, nodeUuid, sk string) (models.NodeQuota, error) {
	resp, err := s.DB.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.TableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: models.NodeQuotaPK(nodeUuid)},
			"sk": &types.AttributeValueMemberS{Value: sk},
		},
		// Policy is read to make a spending decision, and an operator who has
		// just tightened a cap expects the next run to honour it.
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return models.NodeQuota{}, fmt.Errorf("getting node quota %s/%s: %w", nodeUuid, sk, err)
	}
	if len(resp.Item) == 0 {
		return models.NodeQuota{}, nil
	}

	var quota models.NodeQuota
	if err := attributevalue.UnmarshalMap(resp.Item, &quota); err != nil {
		return models.NodeQuota{}, fmt.Errorf("unmarshalling node quota %s/%s: %w", nodeUuid, sk, err)
	}
	return quota, nil
}

func (s *NodeQuotaDatabaseStore) ListForNode(ctx context.Context, nodeUuid string) ([]models.NodeQuota, error) {
	var out []models.NodeQuota
	var startKey map[string]types.AttributeValue

	for {
		resp, err := s.DB.Query(ctx, &dynamodb.QueryInput{
			TableName:              aws.String(s.TableName),
			KeyConditionExpression: aws.String("pk = :pk AND begins_with(sk, :prefix)"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":pk":     &types.AttributeValueMemberS{Value: models.NodeQuotaPK(nodeUuid)},
				":prefix": &types.AttributeValueMemberS{Value: models.QuotaSKPrefix()},
			},
			ConsistentRead:    aws.Bool(true),
			ExclusiveStartKey: startKey,
		})
		if err != nil {
			return nil, fmt.Errorf("listing node quotas for %s: %w", nodeUuid, err)
		}

		var page []models.NodeQuota
		if err := attributevalue.UnmarshalListOfMaps(resp.Items, &page); err != nil {
			return nil, fmt.Errorf("unmarshalling node quotas for %s: %w", nodeUuid, err)
		}
		out = append(out, page...)

		if resp.LastEvaluatedKey == nil || len(resp.LastEvaluatedKey) == 0 {
			break
		}
		startKey = resp.LastEvaluatedKey
	}

	return out, nil
}

func (s *NodeQuotaDatabaseStore) Delete(ctx context.Context, nodeUuid, sk string) error {
	if _, err := s.DB.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.TableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: models.NodeQuotaPK(nodeUuid)},
			"sk": &types.AttributeValueMemberS{Value: sk},
		},
	}); err != nil {
		return fmt.Errorf("deleting node quota %s/%s: %w", nodeUuid, sk, err)
	}
	return nil
}
