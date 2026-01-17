package models

type Account struct {
	Uuid        string `json:"uuid"`
	AccountId   string `json:"accountId"`
	AccountType string `json:"accountType"`
	RoleName    string `json:"roleName"`
	ExternalId  string `json:"externalId"`
	UserId      string `json:"userId"`
}

type AccountResponse struct {
	Uuid string `json:"uuid"`
}

type AccountWorkspaceEnablement struct {
	AccountUuid    string `json:"accountUuid"`
	OrganizationId string `json:"organizationId"`
	IsPublic       bool   `json:"isPublic"`
	EnabledBy      string `json:"enabledBy"`
	EnabledAt      int64  `json:"enabledAt"`
}

type AccountWithWorkspaces struct {
	Account
	EnabledWorkspaces []AccountWorkspaceEnablement `json:"enabledWorkspaces,omitempty"`
}
