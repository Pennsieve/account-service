package models

type Account struct {
	AccountId   string `json:"accountId"`
	AccountType string `json:"accountType"`
	RoleName    string `json:"roleName"`
	ExternalId  string `json:"externalId"`
}
