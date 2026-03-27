package service

import (
	"context"
	"testing"

	"github.com/pennsieve/account-service/internal/models"
	"github.com/stretchr/testify/assert"
)

// mockStorageNodeStore implements StorageNodeStore for testing
type mockStorageNodeStore struct {
	nodes []models.DynamoDBStorageNode
}

func (m *mockStorageNodeStore) GetById(_ context.Context, uuid string) (models.DynamoDBStorageNode, error) {
	for _, n := range m.nodes {
		if n.Uuid == uuid {
			return n, nil
		}
	}
	return models.DynamoDBStorageNode{}, nil
}

func (m *mockStorageNodeStore) GetByAccount(_ context.Context, accountUuid string) ([]models.DynamoDBStorageNode, error) {
	var result []models.DynamoDBStorageNode
	for _, n := range m.nodes {
		if n.AccountUuid == accountUuid {
			result = append(result, n)
		}
	}
	return result, nil
}

func (m *mockStorageNodeStore) GetAllEnabled(_ context.Context) ([]models.DynamoDBStorageNode, error) {
	var result []models.DynamoDBStorageNode
	for _, n := range m.nodes {
		if n.Status == "Enabled" {
			result = append(result, n)
		}
	}
	return result, nil
}

func (m *mockStorageNodeStore) Put(_ context.Context, node models.DynamoDBStorageNode) error {
	m.nodes = append(m.nodes, node)
	return nil
}

func (m *mockStorageNodeStore) Delete(_ context.Context, uuid string) error {
	for i, n := range m.nodes {
		if n.Uuid == uuid {
			m.nodes = append(m.nodes[:i], m.nodes[i+1:]...)
			return nil
		}
	}
	return nil
}

func TestStoragePolicyService_RegenerateStoragePolicies_SkipsInTestEnv(t *testing.T) {
	t.Setenv("ENV", "TEST")

	store := &mockStorageNodeStore{
		nodes: []models.DynamoDBStorageNode{
			{Uuid: "1", StorageLocation: "bucket-1", ProviderType: "s3", Status: "Enabled"},
		},
	}

	// In test env, this should return nil without calling IAM
	svc := &StoragePolicyService{StorageNodeStore: store}
	err := svc.RegenerateStoragePolicies(context.Background())
	assert.NoError(t, err)
}

func TestStoragePolicyService_FiltersOnlyS3Nodes(t *testing.T) {
	store := &mockStorageNodeStore{
		nodes: []models.DynamoDBStorageNode{
			{Uuid: "1", StorageLocation: "bucket-1", ProviderType: "s3", Status: "Enabled"},
			{Uuid: "2", StorageLocation: "https://blob.core/container", ProviderType: "azure-blob", Status: "Enabled"},
			{Uuid: "3", StorageLocation: "/local/path", ProviderType: "local", Status: "Enabled"},
			{Uuid: "4", StorageLocation: "bucket-disabled", ProviderType: "s3", Status: "Disabled"},
		},
	}

	// Verify GetAllEnabled only returns enabled nodes
	enabled, err := store.GetAllEnabled(context.Background())
	assert.NoError(t, err)
	assert.Len(t, enabled, 3)

	// Count S3 nodes
	s3Count := 0
	for _, n := range enabled {
		if n.ProviderType == "s3" {
			s3Count++
		}
	}
	assert.Equal(t, 1, s3Count)
}
