package store_dynamodb

import (
	"context"
	"log"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go/aws"
)

type DynamoDBStore interface {
	Insert(context.Context, Account) error
	GetById(context.Context, string) (Account, error)
	Get(context.Context) ([]Account, error)
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
		log.Printf("couldn't perform insert. Here's why: %v\n", err)
		return err
	}
	_, err = r.DB.PutItem(context.TODO(), &dynamodb.PutItemInput{
		TableName: aws.String(r.TableName), Item: item,
	})
	if err != nil {
		log.Printf("couldn't add account information to table. Here's why: %v\n", err)
		return err
	}

	return nil
}

func (r *AccountDatabaseStore) GetById(ctx context.Context, uuid string) (Account, error) {
	account := Account{Uuid: uuid}
	response, err := r.DB.GetItem(ctx, &dynamodb.GetItemInput{
		Key: account.GetKey(), TableName: aws.String(r.TableName),
	})
	if err != nil {
		log.Printf("couldn't get info about %v. Here's why: %v\n", uuid, err)
		return account, err
	}

	err = attributevalue.UnmarshalMap(response.Item, &account)
	if err != nil {
		log.Printf("couldn't unmarshal response. Here's why: %v\n", err)
		return account, err
	}

	return account, nil
}

func (r *AccountDatabaseStore) Get(ctx context.Context) ([]Account, error) {
	accounts := []Account{}

	response, err := r.DB.Scan(ctx, &dynamodb.ScanInput{
		TableName: aws.String(r.TableName),
	})
	if err != nil {
		log.Printf("couldn't get accounts. Here's why: %v\n", err)
		return accounts, err
	}

	err = attributevalue.UnmarshalListOfMaps(response.Items, &accounts)
	if err != nil {
		log.Printf("couldn't unmarshal accounts. Here's why: %v\n", err)
		return accounts, err
	}

	return accounts, nil
}
