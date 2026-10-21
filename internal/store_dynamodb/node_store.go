package store_dynamodb

import (
	"context"
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/pennsieve/account-service/internal/models"
)

type NodeStore interface {
	GetById(context.Context, string) (models.DynamoDBNode, error)
	Get(context.Context, string) ([]models.DynamoDBNode, error)
	GetByAccount(context.Context, string) ([]models.DynamoDBNode, error)
	GetAllEnabled(context.Context) ([]models.DynamoDBNode, error)
	Put(context.Context, models.DynamoDBNode) error
	UpdateStatus(context.Context, string, string) error
	UpdateHealthStatus(ctx context.Context, uuid string, healthStatus string, lastHealthCheck string) error
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

	return withTenant(node), nil
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

	return withTenants(nodes), nil
}

// Put stores node, with organizationId derived from its tenant. A node read
// before it had a tenant gets one from its organizationId and userId.
func (r *NodeDatabaseStore) Put(ctx context.Context, node models.DynamoDBNode) error {
	node, err := node.WithTenant()
	if err != nil {
		return fmt.Errorf("error resolving tenant of node %s: %w", node.Uuid, err)
	}
	node.OrganizationId = models.LegacyOrganizationId(node.Tenant)

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

func (r *NodeDatabaseStore) GetByAccount(ctx context.Context, accountUuid string) ([]models.DynamoDBNode, error) {
	nodes := []models.DynamoDBNode{}
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
		ProjectionExpression:      expr.Projection(),
		TableName:                 aws.String(r.TableName),
	})
	if err != nil {
		return nodes, fmt.Errorf("error getting nodes by account: %w", err)
	}

	err = attributevalue.UnmarshalListOfMaps(response.Items, &nodes)
	if err != nil {
		return nodes, fmt.Errorf("error unmarshaling nodes: %w", err)
	}

	return withTenants(nodes), nil
}

func (r *NodeDatabaseStore) UpdateStatus(ctx context.Context, uuid string, status string) error {
	node := models.DynamoDBNode{Uuid: uuid}
	update := expression.Set(expression.Name("status"), expression.Value(status))
	expr, err := expression.NewBuilder().WithUpdate(update).Build()
	if err != nil {
		return fmt.Errorf("error building update expression: %w", err)
	}

	_, err = r.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		Key:                       node.GetKey(),
		TableName:                 aws.String(r.TableName),
		UpdateExpression:          expr.Update(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	if err != nil {
		return fmt.Errorf("error updating node status: %w", err)
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

func (r *NodeDatabaseStore) GetAllEnabled(ctx context.Context) ([]models.DynamoDBNode, error) {
	var nodes []models.DynamoDBNode
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
			return nodes, fmt.Errorf("error scanning enabled nodes: %w", err)
		}

		var page []models.DynamoDBNode
		err = attributevalue.UnmarshalListOfMaps(response.Items, &page)
		if err != nil {
			return nodes, fmt.Errorf("error unmarshaling nodes: %w", err)
		}
		nodes = append(nodes, page...)

		if response.LastEvaluatedKey == nil {
			break
		}
		lastKey = response.LastEvaluatedKey
	}

	return withTenants(nodes), nil
}

func (r *NodeDatabaseStore) UpdateHealthStatus(ctx context.Context, uuid string, healthStatus string, lastHealthCheck string) error {
	node := models.DynamoDBNode{Uuid: uuid}
	update := expression.Set(
		expression.Name("healthStatus"), expression.Value(healthStatus),
	).Set(
		expression.Name("lastHealthCheck"), expression.Value(lastHealthCheck),
	)
	expr, err := expression.NewBuilder().WithUpdate(update).Build()
	if err != nil {
		return fmt.Errorf("error building update expression: %w", err)
	}

	_, err = r.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		Key:                       node.GetKey(),
		TableName:                 aws.String(r.TableName),
		UpdateExpression:          expr.Update(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	if err != nil {
		return fmt.Errorf("error updating node health status: %w", err)
	}

	return nil
}
// withTenant sets the tenant of a node stored before nodes had one. A node
// whose organizationId and userId don't name a tenant is returned as read,
// so one bad record doesn't hide the rest of a listing; storing it fails.
func withTenant(node models.DynamoDBNode) models.DynamoDBNode {
	withT, err := node.WithTenant()
	if err != nil {
		log.Printf("compute node %s has no tenant: %v", node.Uuid, err)
		return node
	}
	return withT
}

func withTenants(nodes []models.DynamoDBNode) []models.DynamoDBNode {
	for i := range nodes {
		nodes[i] = withTenant(nodes[i])
	}
	return nodes
}
