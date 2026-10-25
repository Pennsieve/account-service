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
	// Connection is how the account's role was made, for its owner:
	// "cloudformation" (the published template, TemplateVersion) or "legacy"
	// (before it, or by an agent without it). LatestTemplateVersion is the
	// published template's, so an older one can show "update available".
	Connection            string `json:"connection,omitempty"`
	TemplateVersion       string `json:"templateVersion,omitempty"`
	LatestTemplateVersion string `json:"latestTemplateVersion,omitempty"`
}

// How an account's role was made (Account.Connection).
const (
	ConnectionCloudFormation = "cloudformation"
	ConnectionLegacy         = "legacy"
)

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
