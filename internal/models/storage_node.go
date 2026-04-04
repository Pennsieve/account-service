package models

import (
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// CreateStorageNodeRequest is the request body for POST /storage-nodes
type CreateStorageNodeRequest struct {
	AccountUuid      string `json:"accountUuid"`
	OrganizationId   string `json:"organizationId"`             // Required: workspace for this storage node
	Name             string `json:"name"`
	Description      string `json:"description"`
	StorageLocation  string `json:"storageLocation"`
	Region           string `json:"region,omitempty"`
	ProviderType     string `json:"providerType"`
	DeploymentMode   string `json:"deploymentMode,omitempty"`   // "basic" (default) or "compliant"
	SkipProvisioning bool   `json:"skipProvisioning,omitempty"` // true to register existing bucket without provisioning
}

// StorageNode is the API response representation of a storage node
type StorageNode struct {
	Uuid            string                          `json:"uuid"`
	Name            string                          `json:"name"`
	Description     string                          `json:"description"`
	AccountUuid     string                          `json:"accountUuid"`
	StorageLocation string                          `json:"storageLocation"`
	Region          string                          `json:"region,omitempty"`
	ProviderType    string                          `json:"providerType"`
	Status          string                          `json:"status"`
	CreatedAt       string                          `json:"createdAt"`
	CreatedBy       string                          `json:"createdBy"`
	Workspaces      []StorageNodeWorkspaceEnablement `json:"workspaces,omitempty"`
}

// StorageNodeResponse is a simple message response for storage node operations
type StorageNodeResponse struct {
	Message string `json:"message"`
}

// StorageNodeUpdateRequest is the request body for PATCH /storage-nodes/{id}
type StorageNodeUpdateRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Status      *string `json:"status,omitempty"`
}

// StorageNodeWorkspaceEnablement represents a workspace association for a storage node
type StorageNodeWorkspaceEnablement struct {
	StorageNodeUuid string `json:"storageNodeUuid"`
	WorkspaceId     string `json:"workspaceId"`
	IsDefault       bool   `json:"isDefault"`
	EnabledBy       string `json:"enabledBy"`
	EnabledAt       string `json:"enabledAt"`
}

// DynamoDBStorageNode represents the DynamoDB storage structure for storage nodes
type DynamoDBStorageNode struct {
	Uuid            string `dynamodbav:"uuid"`
	Name            string `dynamodbav:"name"`
	Description     string `dynamodbav:"description"`
	AccountUuid     string `dynamodbav:"accountUuid"`
	StorageLocation string `dynamodbav:"storageLocation"`
	Region          string `dynamodbav:"region"`
	ProviderType    string `dynamodbav:"providerType"`
	Status          string `dynamodbav:"status"`
	CreatedAt       string `dynamodbav:"createdAt"`
	CreatedBy       string `dynamodbav:"createdBy"`
}

func (i DynamoDBStorageNode) GetKey() map[string]types.AttributeValue {
	uuid, err := attributevalue.Marshal(i.Uuid)
	if err != nil {
		panic(err)
	}

	return map[string]types.AttributeValue{"uuid": uuid}
}

// DynamoDBStorageNodeWorkspace represents the DynamoDB record for workspace enablement on a storage node
type DynamoDBStorageNodeWorkspace struct {
	StorageNodeUuid string `dynamodbav:"storageNodeUuid"`
	WorkspaceId     string `dynamodbav:"workspaceId"`
	IsDefault       bool   `dynamodbav:"isDefault"`
	EnabledBy       string `dynamodbav:"enabledBy"`
	EnabledAt       string `dynamodbav:"enabledAt"`
}

func (i DynamoDBStorageNodeWorkspace) GetKey() map[string]types.AttributeValue {
	storageNodeUuid, err := attributevalue.Marshal(i.StorageNodeUuid)
	if err != nil {
		panic(err)
	}

	workspaceId, err := attributevalue.Marshal(i.WorkspaceId)
	if err != nil {
		panic(err)
	}

	return map[string]types.AttributeValue{
		"storageNodeUuid": storageNodeUuid,
		"workspaceId":     workspaceId,
	}
}
