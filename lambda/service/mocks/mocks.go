package mocks

import (
	"context"

	"github.com/pennsieve/account-service/service/store_dynamodb"
)

type MockDynamoDBStore struct{}

func (r *MockDynamoDBStore) Insert(ctx context.Context, account store_dynamodb.Account) error {

	return nil
}

func (r *MockDynamoDBStore) GetById(ctx context.Context, account string) (store_dynamodb.Account, error) {

	return store_dynamodb.Account{}, nil
}

func NewMockDynamoDBStore() store_dynamodb.DynamoDBStore {
	return &MockDynamoDBStore{}
}
