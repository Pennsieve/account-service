package compute

import (
	"testing"

	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/stretchr/testify/assert"
)

func TestValidateAccountUuid(t *testing.T) {
	tests := []struct {
		name        string
		accountUuid string
		wantErr     bool
	}{
		{"Valid UUID", "account-123", false},
		{"Empty string", "", true},
		{"Whitespace only", "   ", true},
		{"Valid with spaces", " account-123 ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAccountUuid(tt.accountUuid)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, ErrAccountUuidRequired, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateAccountOwnership(t *testing.T) {
	tests := []struct {
		name     string
		account  store_dynamodb.Account
		userId   string
		wantErr  bool
	}{
		{
			name: "Owner matches",
			account: store_dynamodb.Account{
				UserId: "user-123",
			},
			userId:  "user-123",
			wantErr: false,
		},
		{
			name: "Owner does not match",
			account: store_dynamodb.Account{
				UserId: "owner-456",
			},
			userId:  "user-123",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAccountOwnership(tt.account, tt.userId)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, ErrOnlyAccountOwnerCanCreateNodes, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateWorkspaceEnablement(t *testing.T) {
	tests := []struct {
		name         string
		enablement   *store_dynamodb.AccountWorkspace
		accountUuid  string
		orgId        string
		wantErr      bool
	}{
		{
			name: "Valid enablement",
			enablement: &store_dynamodb.AccountWorkspace{
				AccountUuid: "account-123",
				WorkspaceId: "org-456",
				IsPublic:    false,
			},
			accountUuid: "account-123",
			orgId:       "org-456",
			wantErr:     false,
		},
		{
			name:        "No enablement",
			enablement:  nil,
			accountUuid: "account-123",
			orgId:       "org-456",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWorkspaceEnablement(tt.enablement, tt.accountUuid, tt.orgId)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, ErrAccountNotEnabledForWorkspace, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateUserIds(t *testing.T) {
	tests := []struct {
		name           string
		userId         string
		organizationId string
		wantUserIdInt  int64
		wantOrgIdInt   int64
		wantErr        bool
		expectedErr    error
	}{
		{
			name:           "Valid IDs",
			userId:         "123",
			organizationId: "456",
			wantUserIdInt:  123,
			wantOrgIdInt:   456,
			wantErr:        false,
		},
		{
			name:           "Invalid user ID",
			userId:         "not-a-number",
			organizationId: "456",
			wantErr:        true,
			expectedErr:    ErrInvalidUserIdFormat,
		},
		{
			name:           "Invalid organization ID",
			userId:         "123",
			organizationId: "not-a-number",
			wantErr:        true,
			expectedErr:    ErrInvalidOrganizationIdFormat,
		},
		{
			name:           "Both invalid",
			userId:         "not-a-number",
			organizationId: "also-not-a-number",
			wantErr:        true,
			expectedErr:    ErrInvalidUserIdFormat, // Returns first error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userIdInt, orgIdInt, err := validateUserIds(tt.userId, tt.organizationId)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedErr, err)
				assert.Equal(t, int64(0), userIdInt)
				assert.Equal(t, int64(0), orgIdInt)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantUserIdInt, userIdInt)
				assert.Equal(t, tt.wantOrgIdInt, orgIdInt)
			}
		})
	}
}