package models

type Account struct {
	Uuid        string `json:"uuid"`
	AccountId   string `json:"accountId"`
	AccountType string `json:"accountType"`
	RoleName    string `json:"roleName"`
	ExternalId  string `json:"externalId"`
	UserId      string `json:"userId"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"` // "Enabled" or "Paused"
}

type AccountResponse struct {
	Uuid string `json:"uuid"`
}

// AccountWorkspaceEnablement represents the enablement of a workspace on an account
type AccountWorkspaceEnablement struct {
	AccountUuid    string `json:"accountUuid"`
	OrganizationId string `json:"organizationId"`
	// IsPublic determines who can manage resources on this account:
	// - true: workspace admins can manage resources (subject to EnableCompute/EnableStorage)
	// - false: only the account owner can manage resources
	IsPublic      bool   `json:"isPublic"`
	EnableCompute bool   `json:"enableCompute"` // If true and IsPublic, admins can create compute nodes
	EnableStorage bool   `json:"enableStorage"` // If true and IsPublic, admins can create storage nodes
	EnabledBy     string `json:"enabledBy"`
	EnabledAt     int64  `json:"enabledAt"`
}

type AccountWithWorkspaces struct {
	Account
	EnabledWorkspaces []AccountWorkspaceEnablement `json:"enabledWorkspaces,omitempty"`
}
