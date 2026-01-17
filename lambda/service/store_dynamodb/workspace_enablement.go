package store_dynamodb

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go/aws"
)

type WorkspaceEnablementStore interface {
	Insert(context.Context, AccountWorkspaceEnablement) error
	Delete(context.Context, string, string) error
	GetByAccount(context.Context, string) ([]AccountWorkspaceEnablement, error)
	GetByOrganization(context.Context, string) ([]AccountWorkspaceEnablement, error)
	Get(context.Context, string, string) (AccountWorkspaceEnablement, error)
}

type WorkspaceEnablementDatabaseStore struct {
	DB        *dynamodb.Client
	TableName string
}

func NewWorkspaceEnablementDatabaseStore(db *dynamodb.Client, tableName string) WorkspaceEnablementStore {
	return &WorkspaceEnablementDatabaseStore{db, tableName}
}

func (r *WorkspaceEnablementDatabaseStore) Insert(ctx context.Context, enablement AccountWorkspaceEnablement) error {
	item, err := attributevalue.MarshalMap(enablement)
	if err != nil {
		return fmt.Errorf("error marshaling workspace enablement: %w", err)
	}
	_, err = r.DB.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(r.TableName), Item: item,
	})
	if err != nil {
		return fmt.Errorf("error inserting workspace enablement: %w", err)
	}

	return nil
}

func (r *WorkspaceEnablementDatabaseStore) Delete(ctx context.Context, accountUuid string, organizationId string) error {
	enablement := AccountWorkspaceEnablement{
		AccountUuid:    accountUuid,
		OrganizationId: organizationId,
	}
	_, err := r.DB.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		Key:       enablement.GetKey(),
		TableName: aws.String(r.TableName),
	})
	if err != nil {
		return fmt.Errorf("error deleting workspace enablement: %w", err)
	}
	return nil
}

func (r *WorkspaceEnablementDatabaseStore) GetByAccount(ctx context.Context, accountUuid string) ([]AccountWorkspaceEnablement, error) {
	var enablements []AccountWorkspaceEnablement

	expr, err := expression.NewBuilder().WithKeyCondition(
		expression.Key("accountUuid").Equal(expression.Value(accountUuid)),
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
		return enablements, fmt.Errorf("error querying workspace enablements: %w", err)
	}

	err = attributevalue.UnmarshalListOfMaps(response.Items, &enablements)
	if err != nil {
		return enablements, fmt.Errorf("error unmarshaling workspace enablements: %w", err)
	}

	return enablements, nil
}

func (r *WorkspaceEnablementDatabaseStore) GetByOrganization(ctx context.Context, organizationId string) ([]AccountWorkspaceEnablement, error) {
	var enablements []AccountWorkspaceEnablement

	expr, err := expression.NewBuilder().WithKeyCondition(
		expression.Key("organizationId").Equal(expression.Value(organizationId)),
	).Build()
	if err != nil {
		return enablements, fmt.Errorf("error building expression: %w", err)
	}

	response, err := r.DB.Query(ctx, &dynamodb.QueryInput{
		TableName:                 aws.String(r.TableName),
		IndexName:                 aws.String("organizationId-index"),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		KeyConditionExpression:    expr.KeyCondition(),
	})
	if err != nil {
		return enablements, fmt.Errorf("error querying workspace enablements: %w", err)
	}

	err = attributevalue.UnmarshalListOfMaps(response.Items, &enablements)
	if err != nil {
		return enablements, fmt.Errorf("error unmarshaling workspace enablements: %w", err)
	}

	return enablements, nil
}

func (r *WorkspaceEnablementDatabaseStore) Get(ctx context.Context, accountUuid string, organizationId string) (AccountWorkspaceEnablement, error) {
	enablement := AccountWorkspaceEnablement{
		AccountUuid:    accountUuid,
		OrganizationId: organizationId,
	}
	
	response, err := r.DB.GetItem(ctx, &dynamodb.GetItemInput{
		Key:       enablement.GetKey(),
		TableName: aws.String(r.TableName),
	})
	if err != nil {
		return AccountWorkspaceEnablement{}, fmt.Errorf("error getting workspace enablement: %w", err)
	}
	if response.Item == nil {
		return AccountWorkspaceEnablement{}, nil
	}

	err = attributevalue.UnmarshalMap(response.Item, &enablement)
	if err != nil {
		return enablement, fmt.Errorf("error unmarshaling workspace enablement: %w", err)
	}

	return enablement, nil
}