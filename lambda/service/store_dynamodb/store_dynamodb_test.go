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
		Uuid:           registeredAccountId,
		UserId:         "SomeId",
		OrganizationId: "SomeOrgId",
		AccountId:      "SomeAccountId",
		AccountType:    "aws",
		RoleName:       "SomeRoleName",
		ExternalId:     "SomeExternalId",
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

func TestInsertAndGet(t *testing.T) {
	tableName := "accounts"
	dynamoDBClient := getClient()

	// create table
	_, err := CreateAccountsTable(dynamoDBClient, tableName)
	if err != nil {
		t.Fatalf("err creating table")
	}
	dynamo_store := store_dynamodb.NewAccountDatabaseStore(dynamoDBClient, tableName)

	organizationId := "SomeOrgId"
	uuids := []string{uuid.New().String(), uuid.New().String()}
	for _, u := range uuids {
		store_account := store_dynamodb.Account{
			Uuid:           u,
			UserId:         "SomeId",
			OrganizationId: organizationId,
			AccountId:      "SomeAccountId",
			AccountType:    "aws",
			RoleName:       "SomeRoleName",
			ExternalId:     "SomeExternalId",
		}
		err = dynamo_store.Insert(context.Background(), store_account)
		if err != nil {
			t.Errorf("error inserting item into table")
		}
	}

	accounts, err := dynamo_store.Get(context.Background(), organizationId)
	if err != nil {
		t.Errorf("error getting items")
	}

	if len(accounts) != len(uuids) {
		t.Errorf("expected %v accounts, not %v", len(uuids), len(accounts))
	}

	// delete table
	err = DeleteTable(dynamoDBClient, tableName)
	if err != nil {
		t.Fatalf("err creating table")
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
		BillingMode: "PAY_PER_REQUEST",
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
