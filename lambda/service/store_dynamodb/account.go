package store_dynamodb

import (
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type Account struct {
	Uuid          string      `dynamodbav:"uuid"`
	ApplicationId int64       `dynamodbav:"applicationId"`
	DatasetNodeId string      `dynamodbav:"datasetNodeId"`
	PackageIds    []string    `dynamodbav:"packageIds"`
	Params        interface{} `dynamodbav:"params"`
}

func (i Account) GetKey() map[string]types.AttributeValue {
	uuid, err := attributevalue.Marshal(i.Uuid)
	if err != nil {
		panic(err)
	}

	return map[string]types.AttributeValue{"uuid": uuid}
}
