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
    Name        string `dynamodbav:"name"`
    Description string `dynamodbav:"description"`
    Status      string `dynamodbav:"status"` // "Enabled" or "Paused"
}

// AccountWorkspace represents the DynamoDB record for workspace enablement on an account
type AccountWorkspace struct {
    AccountUuid    string `dynamodbav:"accountUuid"`
    WorkspaceId    string `dynamodbav:"workspaceId"`  // This maps to the organizationId in the application logic
    // IsPublic determines who can create compute nodes on this account:
    // - true: workspace managers can create compute nodes on this account
    // - false: only the account owner can create compute nodes on this account
    IsPublic  bool   `dynamodbav:"isPublic"`
    EnabledBy string `dynamodbav:"enabledBy"`
    EnabledAt int64  `dynamodbav:"enabledAt"`
}

func (i Account) GetKey() map[string]types.AttributeValue {
    uuid, err := attributevalue.Marshal(i.Uuid)
    if err != nil {
        panic(err)
    }

    return map[string]types.AttributeValue{"uuid": uuid}
}

func (i AccountWorkspace) GetKey() map[string]types.AttributeValue {
    accountUuid, err := attributevalue.Marshal(i.AccountUuid)
    if err != nil {
        panic(err)
    }

    workspaceId, err := attributevalue.Marshal(i.WorkspaceId)
    if err != nil {
        panic(err)
    }

    return map[string]types.AttributeValue{
        "accountUuid":    accountUuid,
        "workspaceId": workspaceId,
    }
}
