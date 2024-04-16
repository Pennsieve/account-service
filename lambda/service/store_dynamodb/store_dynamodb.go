package store_dynamodb

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go/aws"
)

type DynamoDBStore interface {
	Insert(context.Context, Account) error
	GetById(context.Context, string) (Account, error)
	Get(context.Context, string) ([]Account, error)
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
		return account, fmt.Errorf("error getting account: %w", err)
	}

	err = attributevalue.UnmarshalMap(response.Item, &account)
	if err != nil {
		return account, fmt.Errorf("error unmarshaling account: %w", err)
	}

	return account, nil
}

func (r *AccountDatabaseStore) Get(ctx context.Context, filter string) ([]Account, error) {
	accounts := []Account{}
	filt := expression.Name("organizationId").Equal((expression.Value(filter)))
	expr, err := expression.NewBuilder().WithFilter(filt).Build()
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

	return accounts, nil
}
