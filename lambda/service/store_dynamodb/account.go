package store_dynamodb

import (
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type Account struct {
	Uuid           string `dynamodbav:"uuid"`
	UserId         string `dynamodbav:"userId"`
	OrganizationId string `dynamodbav:"organizationId"`
	AccountId      string `dynamodbav:"accountId"`
	AccountType    string `dynamodbav:"accountType"`
	RoleName       string `dynamodbav:"roleName"`
	ExternalId     string `dynamodbav:"externalId"`
}

func (i Account) GetKey() map[string]types.AttributeValue {
	uuid, err := attributevalue.Marshal(i.Uuid)
	if err != nil {
		panic(err)
	}

	return map[string]types.AttributeValue{"uuid": uuid}
}
