package compute

import (
	"strconv"
	"strings"

	"github.com/pennsieve/account-service/internal/store_dynamodb"
)

// validateAccountUuid checks if account UUID is provided
func validateAccountUuid(accountUuid string) error {
	if strings.TrimSpace(accountUuid) == "" {
		return ErrAccountUuidRequired
	}
	return nil
}

// validateAccountOwnership checks if user owns the account (for private accounts)
func validateAccountOwnership(account store_dynamodb.Account, userId string) error {
	if account.UserId != userId {
		return ErrOnlyAccountOwnerCanCreateNodes
	}
	return nil
}

// validateWorkspaceEnablement checks if account is enabled for the workspace
func validateWorkspaceEnablement(enablement *store_dynamodb.AccountWorkspace, accountUuid, organizationId string) error {
	if enablement == nil {
		return ErrAccountNotEnabledForWorkspace
	}
	return nil
}

// validateUserIds checks if user ID and organization ID are valid integers
func validateUserIds(userId, organizationId string) (int64, int64, error) {
	userIdInt, err := strconv.ParseInt(userId, 10, 64)
	if err != nil {
		return 0, 0, ErrInvalidUserIdFormat
	}
	
	orgIdInt, err := strconv.ParseInt(organizationId, 10, 64)
	if err != nil {
		return 0, 0, ErrInvalidOrganizationIdFormat
	}
	
	return userIdInt, orgIdInt, nil
}

// Custom errors for validation
var (
	ErrAccountUuidRequired           = &ValidationError{"Account UUID is required"}
	ErrOnlyAccountOwnerCanCreateNodes = &ValidationError{"Only account owner can create nodes"}
	ErrAccountNotEnabledForWorkspace = &ValidationError{"Account not enabled for workspace"}
	ErrInvalidUserIdFormat          = &ValidationError{"Invalid user ID format"}
	ErrInvalidOrganizationIdFormat  = &ValidationError{"Invalid organization ID format"}
)

type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}