package errors

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/pennsieve/account-service/internal/models"
)

// Router errors - used by main handler router and compute router
var ErrUnsupportedRoute = errors.New("unsupported route")
var ErrUnsupportedPath = errors.New("unsupported path")

// Account-specific errors
var ErrUnsupportedAccountType = errors.New("unsupported account type")
var ErrMissingAccountUuid = errors.New("missing account uuid")
var ErrMissingWorkspaceId = errors.New("missing workspace id")
var ErrAccountNotFound = errors.New("account not found")
var ErrAccountDoesNotBelongToUser = errors.New("account does not belong to user")
var ErrWorkspaceEnablementNotFound = errors.New("workspace enablement not found")
var ErrAccountAlreadyEnabledForWorkspace = errors.New("account already enabled for workspace")
var ErrAccountHasActiveNodes = errors.New("account has active compute nodes")
var ErrInvalidStatus = errors.New("invalid status value")

// Compute-specific errors
var ErrRunningFargateTask = errors.New("error running Fargate task")
var ErrCreatingNode = errors.New("error creating node in database")
var ErrMissingNodeUuid = errors.New("missing node uuid")
var ErrMissingUserId = errors.New("missing user id")
var ErrMissingTeamId = errors.New("missing team id")
var ErrInvalidAccessScope = errors.New("invalid access scope")
var ErrForbidden = errors.New("forbidden")
var ErrOnlyOwnerCanChangePermissions = errors.New("only owner can change permissions")
var ErrUpdatingPermissions = errors.New("error updating permissions")
var ErrGettingPermissions = errors.New("error getting permissions")
var ErrCheckingAccess = errors.New("error checking access")
var ErrGrantingAccess = errors.New("error granting access")
var ErrRevokingAccess = errors.New("error revoking access")
var ErrCannotRevokeOwnerAccess = errors.New("cannot revoke access from owner")
var ErrNodePending = errors.New("cannot modify status of pending node")
var ErrCannotEnableNodeAccountPaused = errors.New("cannot enable compute node while account is paused")
var ErrOrganizationIndependentNodeCannotBeShared = errors.New("organization-independent nodes cannot be shared")
var ErrCannotAttachNodeWithExistingOrganization = errors.New("cannot attach node that already belongs to an organization")
var ErrAccountNotEnabledForWorkspace = errors.New("account is not enabled for this workspace")
var ErrOnlyAccountOwnerCanCreateNodes = errors.New("only the account owner can create nodes on private accounts")
var ErrOnlyWorkspaceAdminsCanCreateNodes = errors.New("only workspace administrators can create nodes on public accounts")
var ErrOnlyAccountOwnerCanDetachNodes = errors.New("only the account owner can detach nodes")
var ErrOrganizationNotFound = errors.New("organization not found")
var ErrInvalidOrganizationIdFormat = errors.New("invalid organization ID format - expected format: N:organization:uuid")
var ErrBadRequest = errors.New("bad request")
var ErrLLMBaaRequired = errors.New("LLM access in secure/compliant mode requires llmBaaAcknowledged: true (confirming an AWS BAA is in place)")

// Common errors used across handlers
var ErrMarshaling = errors.New("error marshaling item")
var ErrConfig = errors.New("error loading AWS config")
var ErrSTS = errors.New("error performing STS action")
var ErrUnmarshaling = errors.New("error unmarshaling body")
var ErrDynamoDB = errors.New("error performing action on DynamoDB table")
var ErrNoRecordsFound = errors.New("error no records found")
var ErrRecordAlreadyExists = errors.New("error records exists")
var ErrNotFound = errors.New("not found")
var ErrUnauthorized = errors.New("unauthorized")

// HandlerError formats error messages for handlers
func HandlerError(handlerName string, handlerError error) string {
	return fmt.Sprintf("%s: %s", handlerName, handlerError.Error())
}

// ComputeHandlerError formats error messages for compute handlers with JSON response
func ComputeHandlerError(handlerName string, errorMessage error) string {
	log.Printf("%s: %s", handlerName, errorMessage.Error())
	m, err := json.Marshal(models.NodeResponse{
		Message: errorMessage.Error(),
	})
	if err != nil {
		log.Printf("%s: %s", handlerName, err.Error())
		return err.Error()
	}
	return string(m)
}