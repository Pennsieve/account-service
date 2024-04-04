package models

type Account struct {
	Uuid           string `json:"uuid"`
	AccountId      string `json:"accountId"`
	AccountType    string `json:"accountType"`
	RoleName       string `json:"roleName"`
	ExternalId     string `json:"externalId"`
	OrganizationId string `json:"organizationId"`
	UserId         string `json:"userId"`
}

type AccountResponse struct {
	Uuid string `json:"uuid"`
}
