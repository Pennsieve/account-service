package account

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/pennsieve/account-service/internal/store_dynamodb"
)

// ErrRoleCodeNotIssued: the external ID isn't one Pennsieve issued to the user
// registering, for this role, or it has expired or been used.
var ErrRoleCodeNotIssued = errors.New("this role setup wasn't issued to you, or it has expired or been used: run the registration again")

// checkIssuedCode reports whether externalId is a code Pennsieve issued to
// userId for roleName that is unexpired and unused at now. It only reads:
// the code is used up after the role is verified (useIssuedCode), so a
// registration whose verification fails (IAM still propagating) can retry with
// the same code. Store failures come back as other errors.
func checkIssuedCode(ctx context.Context, codes store_dynamodb.RoleCodeStore, externalId, userId, roleName string, now time.Time) error {
	code, found, err := codes.Get(ctx, externalId)
	if err != nil {
		return fmt.Errorf("checking role code: %w", err)
	}
	if !found || code.UserId != userId || code.RoleName != roleName || code.Used || code.ExpiresAt <= now.Unix() {
		return ErrRoleCodeNotIssued
	}
	return nil
}

// useIssuedCode uses up the code for this registration, conditionally, so of
// two registrations racing on one code only one gets past here.
func useIssuedCode(ctx context.Context, codes store_dynamodb.RoleCodeStore, externalId, userId, roleName string, now time.Time) error {
	err := codes.MarkUsed(ctx, externalId, userId, roleName, now.Unix())
	if errors.Is(err, store_dynamodb.ErrRoleCodeUnavailable) {
		return ErrRoleCodeNotIssued
	}
	return err
}
