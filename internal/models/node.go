package models

import (
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// CreateComputeNodeRequest is the request body for POST /compute-nodes
type CreateComputeNodeRequest struct {
	AccountId       string `json:"accountId"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	OrganizationId  string `json:"organizationId,omitempty"`
	ProvisionerImage    string `json:"provisionerImage,omitempty"`
	ProvisionerImageTag string `json:"provisionerImageTag,omitempty"`
	DeploymentMode      string `json:"deploymentMode,omitempty"`
	EnableLLMAccess     bool   `json:"enableLLMAccess,omitempty"`
	LlmBaaAcknowledged  bool   `json:"llmBaaAcknowledged,omitempty"`
}

// Node is the response representation of a compute node
type Node struct {
	Uuid                  string          `json:"uuid"`
	Name                  string          `json:"name"`
	Description           string          `json:"description"`
	QueueUrl              string          `json:"queueUrl"`
	Account               NodeAccount     `json:"account"`
	CreatedAt             string          `json:"createdAt"`
	OrganizationId        string          `json:"organizationId,omitempty"`
	OwnerId               string          `json:"ownerId"`
	Identifier            string          `json:"identifier"`
	WorkflowManagerTag    string          `json:"workflowManagerTag"`
	ProvisionerImage      string          `json:"provisionerImage,omitempty"`
	ProvisionerImageTag   string          `json:"provisionerImageTag,omitempty"`
	DeploymentMode        string          `json:"deploymentMode,omitempty"`
	EnableLLMAccess       bool            `json:"enableLLMAccess,omitempty"`
	LlmBaaAcknowledged    bool            `json:"llmBaaAcknowledged,omitempty"`
	AccessScope           NodeAccessScope `json:"accessScope,omitempty"`
	Status                string          `json:"status"`
}

type NodeAccount struct {
	Uuid        string `json:"uuid"`
	AccountId   string `json:"accountId"`
	AccountType string `json:"accountType"`
	OwnerId     string `json:"ownerId,omitempty"` // The owner of the AWS/GCP account
}

type NodeResponse struct {
	Message string `json:"message"`
}

type NodeUpdateRequest struct {
	WorkflowManagerTag    string `json:"workflowManagerTag,omitempty"`    // Optional - only for legacy provisioner
	WorkflowManagerCpu    int    `json:"workflowManagerCpu,omitempty"`    // Optional - only for legacy provisioner
	WorkflowManagerMemory int    `json:"workflowManagerMemory,omitempty"` // Optional - only for legacy provisioner
	AuthorizationType     string `json:"authorizationType,omitempty"`     // Optional - "NONE" or "AWS_IAM", only for legacy provisioner
	ProvisionerImage      string `json:"provisionerImage,omitempty"`      // Optional - Docker image for the provisioner
	ProvisionerImageTag   string `json:"provisionerImageTag,omitempty"`   // Optional - Docker tag for the provisioner image
}

// DynamoDBNode represents the DynamoDB storage structure for compute nodes
type DynamoDBNode struct {
	Uuid                  string `dynamodbav:"uuid"`
	Name                  string `dynamodbav:"name"`
	Description           string `dynamodbav:"description"`
	ComputeNodeGatewayUrl string `dynamodbav:"computeNodeGatewayUrl"`
	EfsId                 string `dynamodbav:"efsId"`
	QueueUrl              string `dynamodbav:"queueUrl"`
	Env                   string `dynamodbav:"environment"`
	AccountUuid           string `dynamodbav:"accountUuid"`
	AccountId             string `dynamodbav:"accountId"`
	AccountType           string `dynamodbav:"accountType"`
	CreatedAt             string `dynamodbav:"createdAt"`
	OrganizationId        string `dynamodbav:"organizationId"`
	UserId                string `dynamodbav:"userId"`
	Identifier            string `dynamodbav:"identifier"`
	WorkflowManagerTag    string `dynamodbav:"workflowManagerTag"`
	DeploymentMode        string `dynamodbav:"deploymentMode"`
	EnableLLMAccess       bool   `dynamodbav:"enableLLMAccess"`
	LlmBaaAcknowledged    bool   `dynamodbav:"llmBaaAcknowledged"`
	Status                string `dynamodbav:"status"`
}

func (i DynamoDBNode) GetKey() map[string]types.AttributeValue {
	uuid, err := attributevalue.Marshal(i.Uuid)
	if err != nil {
		panic(err)
	}

	return map[string]types.AttributeValue{"uuid": uuid}
}