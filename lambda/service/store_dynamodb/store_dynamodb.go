package store_dynamodb

import (
	"context"
	"log"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go/aws"
)

type DynamoDBStore interface {
	Insert(context.Context, Integration) error
	GetById(context.Context, string) (Integration, error)
}

type IntegrationDatabaseStore struct {
	DB        *dynamodb.Client
	TableName string
}

func NewIntegrationDatabaseStore(db *dynamodb.Client, tableName string) DynamoDBStore {
	return &IntegrationDatabaseStore{db, tableName}
}

func (r *IntegrationDatabaseStore) Insert(ctx context.Context, integration Integration) error {
	item, err := attributevalue.MarshalMap(integration)
	if err != nil {
		return err
	}
	_, err = r.DB.PutItem(context.TODO(), &dynamodb.PutItemInput{
		TableName: aws.String(r.TableName), Item: item,
	})
	if err != nil {
		log.Printf("couldn't add integration to table. Here's why: %v\n", err)
	}
	return err
}

func (r *IntegrationDatabaseStore) GetById(ctx context.Context, integrationId string) (Integration, error) {
	integration := Integration{Uuid: integrationId}
	response, err := r.DB.GetItem(ctx, &dynamodb.GetItemInput{
		Key: integration.GetKey(), TableName: aws.String(r.TableName),
	})
	if err != nil {
		log.Printf("couldn't get info about %v. Here's why: %v\n", integrationId, err)
	} else {
		err = attributevalue.UnmarshalMap(response.Item, &integration)
		if err != nil {
			log.Printf("couldn't unmarshal response. Here's why: %v\n", err)
		}
	}

	return integration, err
}
