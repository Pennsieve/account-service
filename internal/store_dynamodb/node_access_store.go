package store_dynamodb

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/pennsieve/account-service/internal/models"
)

type NodeAccessStore interface {
	// Grant access to a node
	GrantAccess(ctx context.Context, access models.NodeAccess) error
	// Revoke access to a node
	RevokeAccess(ctx context.Context, entityId, nodeId string) error
	// Check if an entity has access to a node
	HasAccess(ctx context.Context, entityId, nodeId string) (bool, error)
	// Get all entities with access to a node
	GetNodeAccess(ctx context.Context, nodeUuid string) ([]models.NodeAccess, error)
	// Get all nodes an entity has access to
	GetEntityAccess(ctx context.Context, entityId string) ([]models.NodeAccess, error)
	// Get all nodes in an organization with workspace visibility
	GetWorkspaceNodes(ctx context.Context, organizationId string) ([]models.NodeAccess, error)
	// Batch check access for multiple entities (for teams)
	BatchCheckAccess(ctx context.Context, entityIds []string, nodeId string) (bool, error)
	// Remove all access for a node (when deleting)
	RemoveAllNodeAccess(ctx context.Context, nodeUuid string) error
	// Update node access scope
	UpdateNodeAccessScope(ctx context.Context, nodeUuid string, accessScope models.NodeAccessScope, organizationId, grantedBy string) error
}

type NodeAccessDatabaseStore struct {
	DB        *dynamodb.Client
	TableName string
}

func NewNodeAccessDatabaseStore(db *dynamodb.Client, tableName string) NodeAccessStore {
	return &NodeAccessDatabaseStore{DB: db, TableName: tableName}
}

func (s *NodeAccessDatabaseStore) GrantAccess(ctx context.Context, access models.NodeAccess) error {
	access.GrantedAt = time.Now()
	
	item, err := attributevalue.MarshalMap(access)
	if err != nil {
		return fmt.Errorf("error marshaling access: %w", err)
	}

	_, err = s.DB.PutItem(ctx, &dynamodb.PutItemInput{
		Item:      item,
		TableName: aws.String(s.TableName),
	})
	if err != nil {
		return fmt.Errorf("error granting access: %w", err)
	}

	return nil
}

func (s *NodeAccessDatabaseStore) RevokeAccess(ctx context.Context, entityId, nodeId string) error {
	access := models.NodeAccess{
		EntityId: entityId,
		NodeId:   nodeId,
	}

	_, err := s.DB.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		Key:       access.GetKey(),
		TableName: aws.String(s.TableName),
	})
	if err != nil {
		return fmt.Errorf("error revoking access: %w", err)
	}

	return nil
}

func (s *NodeAccessDatabaseStore) HasAccess(ctx context.Context, entityId, nodeId string) (bool, error) {
	access := models.NodeAccess{
		EntityId: entityId,
		NodeId:   nodeId,
	}

	response, err := s.DB.GetItem(ctx, &dynamodb.GetItemInput{
		Key:       access.GetKey(),
		TableName: aws.String(s.TableName),
	})
	if err != nil {
		return false, fmt.Errorf("error checking access: %w", err)
	}

	return response.Item != nil, nil
}

func (s *NodeAccessDatabaseStore) GetNodeAccess(ctx context.Context, nodeUuid string) ([]models.NodeAccess, error) {
	nodeId := models.FormatNodeId(nodeUuid)
	keyCond := expression.Key("nodeId").Equal(expression.Value(nodeId))
	expr, err := expression.NewBuilder().WithKeyCondition(keyCond).Build()
	if err != nil {
		return nil, fmt.Errorf("error building expression: %w", err)
	}

	response, err := s.DB.Query(ctx, &dynamodb.QueryInput{
		IndexName:                 aws.String("nodeId-entityId-index"),
		KeyConditionExpression:    expr.KeyCondition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		TableName:                 aws.String(s.TableName),
	})
	if err != nil {
		return nil, fmt.Errorf("error getting node access: %w", err)
	}

	var accessList []models.NodeAccess
	err = attributevalue.UnmarshalListOfMaps(response.Items, &accessList)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling access list: %w", err)
	}

	return accessList, nil
}

func (s *NodeAccessDatabaseStore) GetEntityAccess(ctx context.Context, entityId string) ([]models.NodeAccess, error) {
	keyCond := expression.Key("entityId").Equal(expression.Value(entityId))
	expr, err := expression.NewBuilder().WithKeyCondition(keyCond).Build()
	if err != nil {
		return nil, fmt.Errorf("error building expression: %w", err)
	}

	response, err := s.DB.Query(ctx, &dynamodb.QueryInput{
		KeyConditionExpression:    expr.KeyCondition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		TableName:                 aws.String(s.TableName),
	})
	if err != nil {
		return nil, fmt.Errorf("error getting entity access: %w", err)
	}

	var accessList []models.NodeAccess
	err = attributevalue.UnmarshalListOfMaps(response.Items, &accessList)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling access list: %w", err)
	}

	return accessList, nil
}

func (s *NodeAccessDatabaseStore) GetWorkspaceNodes(ctx context.Context, organizationId string) ([]models.NodeAccess, error) {
	keyCond := expression.Key("organizationId").Equal(expression.Value(organizationId))
	filter := expression.Name("accessType").Equal(expression.Value(string(models.AccessTypeWorkspace)))
	expr, err := expression.NewBuilder().
		WithKeyCondition(keyCond).
		WithFilter(filter).
		Build()
	if err != nil {
		return nil, fmt.Errorf("error building expression: %w", err)
	}

	response, err := s.DB.Query(ctx, &dynamodb.QueryInput{
		IndexName:                 aws.String("organizationId-nodeId-index"),
		KeyConditionExpression:    expr.KeyCondition(),
		FilterExpression:          expr.Filter(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		TableName:                 aws.String(s.TableName),
	})
	if err != nil {
		return nil, fmt.Errorf("error getting workspace nodes: %w", err)
	}

	var accessList []models.NodeAccess
	err = attributevalue.UnmarshalListOfMaps(response.Items, &accessList)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling access list: %w", err)
	}

	return accessList, nil
}

func (s *NodeAccessDatabaseStore) BatchCheckAccess(ctx context.Context, entityIds []string, nodeId string) (bool, error) {
	for _, entityId := range entityIds {
		hasAccess, err := s.HasAccess(ctx, entityId, nodeId)
		if err != nil {
			return false, err
		}
		if hasAccess {
			return true, nil
		}
	}
	return false, nil
}

func (s *NodeAccessDatabaseStore) RemoveAllNodeAccess(ctx context.Context, nodeUuid string) error {
	// First, get all access entries for this node
	accessList, err := s.GetNodeAccess(ctx, nodeUuid)
	if err != nil {
		return err
	}

	// Delete each access entry
	for _, access := range accessList {
		err = s.RevokeAccess(ctx, access.EntityId, access.NodeId)
		if err != nil {
			return fmt.Errorf("error removing access for entity %s: %w", access.EntityId, err)
		}
	}

	return nil
}

func (s *NodeAccessDatabaseStore) UpdateNodeAccessScope(ctx context.Context, nodeUuid string, accessScope models.NodeAccessScope, organizationId, grantedBy string) error {
	nodeId := models.FormatNodeId(nodeUuid)
	
	// First, remove workspace access if it exists
	workspaceEntityId := models.FormatEntityId(models.EntityTypeWorkspace, organizationId)
	_ = s.RevokeAccess(ctx, workspaceEntityId, nodeId) // Ignore error if doesn't exist
	
	// If setting to workspace access scope, grant workspace access
	if accessScope == models.AccessScopeWorkspace {
		workspaceAccess := models.NodeAccess{
			EntityId:       workspaceEntityId,
			NodeId:         nodeId,
			EntityType:     models.EntityTypeWorkspace,
			EntityRawId:    organizationId,
			NodeUuid:       nodeUuid,
			AccessType:     models.AccessTypeWorkspace,
			OrganizationId: organizationId,
			GrantedBy:      grantedBy,
		}
		return s.GrantAccess(ctx, workspaceAccess)
	}
	
	return nil
}

// Batch operations for efficiency
func (s *NodeAccessDatabaseStore) BatchGrantAccess(ctx context.Context, accesses []models.NodeAccess) error {
	// Process in batches of 25 (DynamoDB limit)
	for i := 0; i < len(accesses); i += 25 {
		end := i + 25
		if end > len(accesses) {
			end = len(accesses)
		}
		
		batch := accesses[i:end]
		requests := make([]types.WriteRequest, 0, len(batch))
		
		for _, access := range batch {
			access.GrantedAt = time.Now()
			item, err := attributevalue.MarshalMap(access)
			if err != nil {
				return fmt.Errorf("error marshaling access: %w", err)
			}
			
			requests = append(requests, types.WriteRequest{
				PutRequest: &types.PutRequest{
					Item: item,
				},
			})
		}
		
		_, err := s.DB.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{
			RequestItems: map[string][]types.WriteRequest{
				s.TableName: requests,
			},
		})
		if err != nil {
			return fmt.Errorf("error batch granting access: %w", err)
		}
	}
	
	return nil
}