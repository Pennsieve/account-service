package account

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/pennsieve/account-service/internal/clients"
)

// RoleAssumer is the part of STS that registration checks a role with.
type RoleAssumer interface {
	AssumeRole(ctx context.Context, in *sts.AssumeRoleInput, optFns ...func(*sts.Options)) (*sts.AssumeRoleOutput, error)
}

// NewRoleAssumer builds the RoleAssumer registration uses; tests replace it.
var NewRoleAssumer = func(cfg aws.Config) RoleAssumer { return sts.NewFromConfig(cfg) }

// verifyRetryDelays are the waits between attempts to assume a role that was
// just created: IAM is eventually consistent, so a new role (or its trust
// policy) can take seconds to be usable. About 18s in all, so the request
// stays inside API Gateway's 30s limit. Tests shorten it.
var verifyRetryDelays = []time.Duration{1 * time.Second, 2 * time.Second, 3 * time.Second, 4 * time.Second, 4 * time.Second, 4 * time.Second}

var (
	// ErrRoleUnusable: Pennsieve could not assume the role.
	ErrRoleUnusable = errors.New("Pennsieve couldn't use the role: check that it exists in this AWS account and trusts Pennsieve")
	// ErrRoleNotRestricted: the role can be assumed without its external ID.
	ErrRoleNotRestricted = errors.New("the role's trust policy doesn't require its external ID: recreate the role with the trust policy Pennsieve provides")
	// ErrRoleAlreadyRegistered: someone already registered this account's role.
	ErrRoleAlreadyRegistered = errors.New("this role is already registered with Pennsieve")
	// ErrInvalidExternalId: not an external ID Pennsieve issues.
	ErrInvalidExternalId = errors.New("invalid external id: use the one returned with the role configuration")
)

// verifyRole checks, before an account is registered, that Pennsieve can use
// its role: assuming it (with the external ID, when there is one) must work,
// and, when there is one, assuming it without must not, which proves the trust
// policy requires it. sleep is time.Sleep outside tests.
func verifyRole(ctx context.Context, assumer RoleAssumer, accountId, roleName, externalId string, sleep func(time.Duration)) error {
	roleArn := fmt.Sprintf("arn:aws:iam::%s:role/%s", accountId, roleName)
	in := &sts.AssumeRoleInput{
		RoleArn:         aws.String(roleArn),
		RoleSessionName: aws.String("pennsieve-account-registration"),
		DurationSeconds: aws.Int32(900),
	}
	if externalId != "" {
		in.ExternalId = aws.String(externalId)
	}

	var err error
	for attempt := 0; ; attempt++ {
		if _, err = assumer.AssumeRole(ctx, in); err == nil {
			break
		}
		if attempt >= len(verifyRetryDelays) || ctx.Err() != nil {
			log.Printf("registration: assuming %s failed: %v", roleArn, err)
			return ErrRoleUnusable
		}
		sleep(verifyRetryDelays[attempt])
	}

	if externalId == "" {
		log.Printf("registration: %s registered with a legacy trust policy (no external id)", roleArn)
		return nil
	}

	without := *in
	without.ExternalId = nil
	if _, err := assumer.AssumeRole(ctx, &without); err == nil {
		log.Printf("registration: %s can be assumed without its external id", roleArn)
		return ErrRoleNotRestricted
	}
	return nil
}

// validExternalId reports whether externalId, when given, is one Pennsieve
// issues; an empty one is a legacy agent's.
func validExternalId(externalId string) bool {
	return externalId == "" || clients.ValidExternalId(externalId)
}
