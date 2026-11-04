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
    // RuntimeRoleName is the narrow runtime role made with RoleName by
    // template v2 (RoleName + "-runtime"); empty for an account on template
    // v1, whose one role is used for everything (see RuntimeRole).
    RuntimeRoleName string `dynamodbav:"runtimeRoleName,omitempty"`
    ExternalId      string `dynamodbav:"externalId"`
    Name        string `dynamodbav:"name"`
    Description string `dynamodbav:"description"`
    Status      string `dynamodbav:"status"` // "Enabled" or "Paused"
    // Connection is "cloudformation" for a role made from the published
    // template (TemplateVersion); empty for older accounts, which read as
    // "legacy" (models.ConnectionLegacy).
    Connection      string `dynamodbav:"connection,omitempty"`
    TemplateVersion string `dynamodbav:"templateVersion,omitempty"`
}

// AccountWorkspace represents the DynamoDB record for workspace enablement on an account
type AccountWorkspace struct {
    AccountUuid    string `dynamodbav:"accountUuid"`
    WorkspaceId    string `dynamodbav:"workspaceId"`  // This maps to the organizationId in the application logic
    // IsPublic determines who can manage resources on this account:
    // - true: workspace admins can manage resources (subject to EnableCompute/EnableStorage)
    // - false: only the account owner can manage resources
    IsPublic      bool   `dynamodbav:"isPublic"`
    EnableCompute bool   `dynamodbav:"enableCompute"` // If true and IsPublic, admins can create compute nodes
    EnableStorage bool   `dynamodbav:"enableStorage"` // If true and IsPublic, admins can create storage nodes
    EnabledBy     string `dynamodbav:"enabledBy"`
    EnabledAt     int64  `dynamodbav:"enabledAt"`
}

// RuntimeRole is the role to assume for anything but provisioning: the
// runtime role when the account has one, otherwise its only role. Provisioner
// tasks keep using RoleName.
func (i Account) RuntimeRole() string {
    if i.RuntimeRoleName != "" {
        return i.RuntimeRoleName
    }
    return i.RoleName
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
