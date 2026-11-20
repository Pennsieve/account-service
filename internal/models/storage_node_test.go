package models

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

func TestStorageNodeRunsProvisioner(t *testing.T) {
	for _, tc := range []struct {
		name string
		node DynamoDBStorageNode
		want bool
	}{
		{"provisioned s3", DynamoDBStorageNode{ProviderType: "s3", Provisioned: true}, true},
		{"registered s3", DynamoDBStorageNode{ProviderType: "s3", Provisioned: false}, false},
		{"provisioned non-s3", DynamoDBStorageNode{ProviderType: "azure-blob", Provisioned: true}, false},
		{"registered non-s3", DynamoDBStorageNode{ProviderType: "local"}, false},
	} {
		if got := tc.node.RunsProvisioner(); got != tc.want {
			t.Errorf("%s: RunsProvisioner() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Records written before the provisioned attribute existed have no such
// attribute. They must read as registered, so the provisioner is never run
// against a bucket it may not have created.
func TestStorageNodeWithoutProvisionedAttributeIsRegistered(t *testing.T) {
	item := map[string]types.AttributeValue{
		"uuid":            &types.AttributeValueMemberS{Value: "node-1"},
		"providerType":    &types.AttributeValueMemberS{Value: "s3"},
		"storageLocation": &types.AttributeValueMemberS{Value: "existing-bucket"},
		"status":          &types.AttributeValueMemberS{Value: "Enabled"},
	}

	var node DynamoDBStorageNode
	if err := attributevalue.UnmarshalMap(item, &node); err != nil {
		t.Fatalf("UnmarshalMap: %v", err)
	}
	if node.Provisioned {
		t.Errorf("Provisioned = true for a record without the attribute, want false")
	}
	if node.RunsProvisioner() {
		t.Errorf("RunsProvisioner() = true for a record without the attribute, want false")
	}
}
