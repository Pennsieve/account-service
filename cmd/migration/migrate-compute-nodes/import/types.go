package main

// DynamoDBNode represents the structure for both old and new compute nodes tables
type DynamoDBNode struct {
	Uuid                  string `dynamodbav:"uuid" json:"uuid"`
	Name                  string `dynamodbav:"name" json:"name"`
	Description           string `dynamodbav:"description" json:"description"`
	ComputeNodeGatewayUrl string `dynamodbav:"computeNodeGatewayUrl" json:"computeNodeGatewayUrl"`
	EfsId                 string `dynamodbav:"efsId" json:"efsId"`
	QueueUrl              string `dynamodbav:"queueUrl" json:"queueUrl"`
	Env                   string `dynamodbav:"environment" json:"environment"`
	AccountUuid           string `dynamodbav:"accountUuid" json:"accountUuid"`
	AccountId             string `dynamodbav:"accountId" json:"accountId"`
	AccountType           string `dynamodbav:"accountType" json:"accountType"`
	CreatedAt             string `dynamodbav:"createdAt" json:"createdAt"`
	OrganizationId        string `dynamodbav:"organizationId" json:"organizationId"`
	UserId                string `dynamodbav:"userId" json:"userId"`
	Identifier            string `dynamodbav:"identifier" json:"identifier"`
	WorkflowManagerTag    string `dynamodbav:"workflowManagerTag" json:"workflowManagerTag"`
	Status                string `dynamodbav:"status,omitempty" json:"status,omitempty"`
	TimeToExist           int64  `dynamodbav:"TimeToExist,omitempty" json:"timeToExist,omitempty"`
}