package store_dynamodb_test

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
	"github.com/pennsieve/account-service/service/store_dynamodb"
)

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getClient() *dynamodb.Client {
	testDBUri := getEnv("DYNAMODB_URL", "http://localhost:8000")

	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("dummy", "dummy_secret", "1234")),
		config.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc(
			func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{URL: testDBUri}, nil
			})),
	)
	if err != nil {
		panic(err)
	}

	svc := dynamodb.NewFromConfig(cfg)
	return svc
}

func TestInsertAndGetById(t *testing.T) {
	tableName := "accounts"
	dynamoDBClient := getClient()

	// create table
	_, err := CreateAccountsTable(dynamoDBClient, tableName)
	if err != nil {
		t.Fatalf("err creating table")
	}
	dynamo_store := store_dynamodb.NewAccountDatabaseStore(dynamoDBClient, tableName)
	id := uuid.New()
	registeredAccountId := id.String()
	store_account := store_dynamodb.Account{
		Uuid:        registeredAccountId,
		UserId:      "SomeId",
		AccountId:   "SomeAccountId",
		AccountType: "aws",
		RoleName:    "SomeRoleName",
		ExternalId:  "SomeExternalId",
	}
	err = dynamo_store.Insert(context.Background(), store_account)
	if err != nil {
		t.Errorf("error inserting item into table")
	}
	accountItem, err := dynamo_store.GetById(context.Background(), registeredAccountId)
	if err != nil {
		t.Errorf("error getting item from table")
	}

	if accountItem.Uuid != registeredAccountId {
		t.Errorf("expected uuid to equal %s", registeredAccountId)
	}

	// delete table
	err = DeleteTable(dynamoDBClient, tableName)
	if err != nil {
		t.Fatalf("err creating table")
	}

}

func TestInsertAndGetByUserId(t *testing.T) {
	tableName := "accounts"
	dynamoDBClient := getClient()

	// create table with userId index
	_, err := CreateAccountsTableWithUserIndex(dynamoDBClient, tableName)
	if err != nil {
		t.Fatalf("err creating table: %v", err)
	}
	
	// Use concrete type to access GetByUserId method
	dynamo_store := &store_dynamodb.AccountDatabaseStore{
		DB:        dynamoDBClient,
		TableName: tableName,
	}

	userId := "SomeUserId"
	uuids := []string{uuid.New().String(), uuid.New().String()}
	for _, u := range uuids {
		store_account := store_dynamodb.Account{
			Uuid:        u,
			UserId:      userId,
			AccountId:   u,
			AccountType: "aws",
			RoleName:    "SomeRoleName",
			ExternalId:  "SomeExternalId",
		}
		err = dynamo_store.Insert(context.Background(), store_account)
		if err != nil {
			t.Errorf("error inserting item into table: %v", err)
		}
	}
	
	// Test GetByUserId method
	accounts, err := dynamo_store.GetByUserId(context.Background(), userId)
	if err != nil {
		t.Errorf("error getting items by userId: %v", err)
	}

	if len(accounts) != len(uuids) {
		t.Errorf("expected %v accounts, not %v", len(uuids), len(accounts))
	}

	// Verify individual account
	account, err := dynamo_store.GetById(context.Background(), uuids[0])
	if err != nil {
		t.Errorf("error getting account by id: %v", err)
	}

	if account.Uuid != uuids[0] {
		t.Errorf("expected account uuid %v, got %v", uuids[0], account.Uuid)
	}

	// delete table
	err = DeleteTable(dynamoDBClient, tableName)
	if err != nil {
		t.Fatalf("err deleting table")
	}

}

func CreateAccountsTable(dynamoDBClient *dynamodb.Client, tableName string) (*types.TableDescription, error) {
	var tableDesc *types.TableDescription
	table, err := dynamoDBClient.CreateTable(context.TODO(), &dynamodb.CreateTableInput{
		AttributeDefinitions: []types.AttributeDefinition{{
			AttributeName: aws.String("uuid"),
			AttributeType: types.ScalarAttributeTypeS,
		}},
		KeySchema: []types.KeySchemaElement{{
			AttributeName: aws.String("uuid"),
			KeyType:       types.KeyTypeHash,
		}},
		TableName:   aws.String(tableName),
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		log.Printf("couldn't create table %v. Here's why: %v\n", tableName, err)
	} else {
		waiter := dynamodb.NewTableExistsWaiter(dynamoDBClient)
		err = waiter.Wait(context.TODO(), &dynamodb.DescribeTableInput{
			TableName: aws.String(tableName)}, 5*time.Minute)
		if err != nil {
			log.Printf("wait for table exists failed. Here's why: %v\n", err)
		}
		tableDesc = table.TableDescription
	}
	return tableDesc, err
}

func CreateAccountsTableWithUserIndex(dynamoDBClient *dynamodb.Client, tableName string) (*types.TableDescription, error) {
	var tableDesc *types.TableDescription
	table, err := dynamoDBClient.CreateTable(context.TODO(), &dynamodb.CreateTableInput{
		AttributeDefinitions: []types.AttributeDefinition{
			{
				AttributeName: aws.String("uuid"),
				AttributeType: types.ScalarAttributeTypeS,
			},
			{
				AttributeName: aws.String("userId"),
				AttributeType: types.ScalarAttributeTypeS,
			},
		},
		KeySchema: []types.KeySchemaElement{{
			AttributeName: aws.String("uuid"),
			KeyType:       types.KeyTypeHash,
		}},
		GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
			{
				IndexName: aws.String("userId-index"),
				KeySchema: []types.KeySchemaElement{
					{
						AttributeName: aws.String("userId"),
						KeyType:       types.KeyTypeHash,
					},
				},
				Projection: &types.Projection{
					ProjectionType: types.ProjectionTypeAll,
				},
			},
		},
		TableName:   aws.String(tableName),
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		log.Printf("couldn't create table %v. Here's why: %v\n", tableName, err)
	} else {
		waiter := dynamodb.NewTableExistsWaiter(dynamoDBClient)
		err = waiter.Wait(context.TODO(), &dynamodb.DescribeTableInput{
			TableName: aws.String(tableName)}, 5*time.Minute)
		if err != nil {
			log.Printf("wait for table exists failed. Here's why: %v\n", err)
		}
		tableDesc = table.TableDescription
	}
	return tableDesc, err
}

func DeleteTable(dynamoDBClient *dynamodb.Client, tableName string) error {
	_, err := dynamoDBClient.DeleteTable(context.TODO(), &dynamodb.DeleteTableInput{
		TableName: aws.String(tableName)})
	if err != nil {
		log.Printf("couldn't delete table %v. Here's why: %v\n", tableName, err)
	}
	return err
}
