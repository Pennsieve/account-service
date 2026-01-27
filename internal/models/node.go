package models

import (
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type Node struct {
	Uuid                  string        `json:"uuid"`
	Name                  string        `json:"name"`
	Description           string        `json:"description"`
	ComputeNodeGatewayUrl string        `json:"computeNodeGatewayUrl"`
	EfsId                 string        `json:"efsId"`
	QueueUrl              string        `json:"queueUrl"`
	Account               NodeAccount   `json:"account"`
	CreatedAt             string        `json:"createdAt"`
	OrganizationId        string        `json:"organizationId,omitempty"` // Optional - empty string means organization-independent
	UserId                string        `json:"userId"`
	Identifier            string        `json:"identifier"`
	WorkflowManagerTag    string        `json:"workflowManagerTag"`
	Status                string        `json:"status"`
}

type NodeAccount struct {
	Uuid        string `json:"uuid"`
	AccountId   string `json:"accountId"`
	AccountType string `json:"accountType"`
}

type NodeResponse struct {
	Message string `json:"message"`
}

type NodeUpdateRequest struct {
	WorkflowManagerTag    string `json:"workflowManagerTag"`
	WorkflowManagerCpu    int    `json:"workflowManagerCpu"`
	WorkflowManagerMemory int    `json:"workflowManagerMemory"`
	AuthorizationType     string `json:"authorizationType"` // "NONE" or "AWS_IAM"
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
	Status                string `dynamodbav:"status"`
}

func (i DynamoDBNode) GetKey() map[string]types.AttributeValue {
	uuid, err := attributevalue.Marshal(i.Uuid)
	if err != nil {
		panic(err)
	}

	return map[string]types.AttributeValue{"uuid": uuid}
}