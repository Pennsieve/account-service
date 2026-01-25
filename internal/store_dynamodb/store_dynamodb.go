package store_dynamodb

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/aws"
)

type DynamoDBStore interface {
	Insert(context.Context, Account) error
	GetById(context.Context, string) (Account, error)
	Get(context.Context, string, map[string]string) ([]Account, error)
	Update(context.Context, Account) error
}

type AccountDatabaseStore struct {
	DB        *dynamodb.Client
	TableName string
}

func NewAccountDatabaseStore(db *dynamodb.Client, tableName string) DynamoDBStore {
	return &AccountDatabaseStore{db, tableName}
}

func (r *AccountDatabaseStore) Insert(ctx context.Context, account Account) error {
	item, err := attributevalue.MarshalMap(account)
	if err != nil {
		return fmt.Errorf("error marshaling account: %w", err)
	}
	_, err = r.DB.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(r.TableName), Item: item,
	})
	if err != nil {
		return fmt.Errorf("error inserting account: %w", err)
	}

	return nil
}

func (r *AccountDatabaseStore) GetById(ctx context.Context, uuid string) (Account, error) {
	account := Account{Uuid: uuid}
	response, err := r.DB.GetItem(ctx, &dynamodb.GetItemInput{
		Key: account.GetKey(), TableName: aws.String(r.TableName),
	})
	if err != nil {
		return Account{}, fmt.Errorf("error getting account: %w", err)
	}
	if response.Item == nil {
		return Account{}, nil
	}

	err = attributevalue.UnmarshalMap(response.Item, &account)
	if err != nil {
		return account, fmt.Errorf("error unmarshaling account: %w", err)
	}

	return account, nil
}

func (r *AccountDatabaseStore) Get(ctx context.Context, organizationId string, params map[string]string) ([]Account, error) {
	accounts := []Account{}
	
	// Legacy method - handles both old (pre-migration) and new (post-migration) data
	// Old records: have organizationId field directly in accounts table
	// New records: use workspace enablement table
	
	// First, try to get old-style records that haven't been migrated yet
	var c expression.ConditionBuilder
	c = expression.Name("organizationId").Equal((expression.Value(organizationId)))
	
	if accountId, found := params["accountId"]; found {
		c = c.And(expression.Name("accountId").Equal((expression.Value(accountId))))
	}
	
	expr, err := expression.NewBuilder().WithFilter(c).Build()
	if err != nil {
		return accounts, fmt.Errorf("error building expression: %w", err)
	}
	
	response, err := r.DB.Scan(ctx, &dynamodb.ScanInput{
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		FilterExpression:          expr.Filter(),
		ProjectionExpression:      expr.Projection(),
		TableName:                 aws.String(r.TableName),
	})
	if err != nil {
		return accounts, fmt.Errorf("error getting accounts: %w", err)
	}
	
	err = attributevalue.UnmarshalListOfMaps(response.Items, &accounts)
	if err != nil {
		return accounts, fmt.Errorf("error unmarshaling accounts: %w", err)
	}
	
	// During migration period, this will return old-style records
	// After migration is complete, this will return empty and the new endpoints should be used
	return accounts, nil
}

func (r *AccountDatabaseStore) GetByUserId(ctx context.Context, userId string) ([]Account, error) {
	accounts := []Account{}

	expr, err := expression.NewBuilder().WithKeyCondition(
		expression.Key("userId").Equal(expression.Value(userId)),
	).Build()
	if err != nil {
		return accounts, fmt.Errorf("error building expression: %w", err)
	}

	response, err := r.DB.Query(ctx, &dynamodb.QueryInput{
		TableName:                 aws.String(r.TableName),
		IndexName:                 aws.String("userId-index"),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		KeyConditionExpression:    expr.KeyCondition(),
	})
	if err != nil {
		return accounts, fmt.Errorf("error getting accounts: %w", err)
	}

	err = attributevalue.UnmarshalListOfMaps(response.Items, &accounts)
	if err != nil {
		return accounts, fmt.Errorf("error unmarshaling accounts: %w", err)
	}

	return accounts, nil
}

func (r *AccountDatabaseStore) Update(ctx context.Context, account Account) error {
	// Use PutItem to update the entire account record
	item, err := attributevalue.MarshalMap(account)
	if err != nil {
		return fmt.Errorf("error marshaling account: %w", err)
	}
	
	_, err = r.DB.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(r.TableName), 
		Item: item,
	})
	if err != nil {
		return fmt.Errorf("error updating account: %w", err)
	}

	return nil
}
