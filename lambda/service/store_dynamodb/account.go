package store_dynamodb

import (
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type Account struct {
	Uuid        string `dynamodbav:"uuid"`
	UserId      string `dynamodbav:"userId"`
	AccountId   string `dynamodbav:"accountId"`
	AccountType string `dynamodbav:"accountType"`
	RoleName    string `dynamodbav:"roleName"`
	ExternalId  string `dynamodbav:"externalId"`
}

type AccountWorkspaceEnablement struct {
	AccountUuid    string `dynamodbav:"accountUuid"`
	OrganizationId string `dynamodbav:"organizationId"`
	IsPublic       bool   `dynamodbav:"isPublic"`
	EnabledBy      string `dynamodbav:"enabledBy"`
	EnabledAt      int64  `dynamodbav:"enabledAt"`
}

func (i Account) GetKey() map[string]types.AttributeValue {
	uuid, err := attributevalue.Marshal(i.Uuid)
	if err != nil {
		panic(err)
	}

	return map[string]types.AttributeValue{"uuid": uuid}
}

func (i AccountWorkspaceEnablement) GetKey() map[string]types.AttributeValue {
	accountUuid, err := attributevalue.Marshal(i.AccountUuid)
	if err != nil {
		panic(err)
	}
	
	organizationId, err := attributevalue.Marshal(i.OrganizationId)
	if err != nil {
		panic(err)
	}

	return map[string]types.AttributeValue{
		"accountUuid":    accountUuid,
		"organizationId": organizationId,
	}
}
