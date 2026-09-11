package storage

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
)

// There are two kinds of storage node and no data fix collapses them: some
// buckets live in a customer's AWS account and are reached by assuming that
// account's cross-account role, and some are the platform's own, sitting in
// the account this service already runs in. Assuming a role into yourself is
// ceremony at best, and a Pennsieve-Compute-* role minted in the platform
// account would be a broad, self-trusting near-admin role created for no
// reason — so platform-owned storage nodes carry an account row naming the
// platform account with NO RoleName, and that is what marks them.
//
// The record decides the path; it does not get to decide it unchecked.
// Every S3 call made on a storage node's bucket carries ExpectedBucketOwner,
// so a record that has drifted fails the call instead of quietly acting on a
// bucket it has misidentified. That costs nothing — it is a parameter on
// calls already being made — and it is what keeps a hand-maintained table
// from being load-bearing.
//
// This matters because such drift is not hypothetical: in prod, both
// platform-owned storage nodes point at an account row carrying the DEV
// platform account's ID. The stakes are bounded — S3 bucket names are
// globally unique, so a wrong account can only make an operation fail, never
// aim it at a different bucket — but "fails loudly at the call" is the
// behaviour we want, not "fails eventually, inside a task, after the API
// answered 202".

var (
	platformAccountOnce sync.Once
	platformAccountID   string
	platformAccountErr  error
)

// PlatformAccountID is the AWS account this service runs in. It cannot change
// for the life of the process, so it is resolved once per container.
func PlatformAccountID(ctx context.Context, cfg aws.Config) (string, error) {
	platformAccountOnce.Do(func() {
		out, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
		if err != nil {
			platformAccountErr = fmt.Errorf("resolving the platform account: %w", err)
			return
		}
		platformAccountID = aws.ToString(out.Account)
	})
	return platformAccountID, platformAccountErr
}

// BucketAccess is how a storage node's bucket should be reached.
type BucketAccess struct {
	// Config carries credentials that can act on the bucket: the service's
	// own for a platform-managed bucket, the assumed cross-account role
	// otherwise.
	Config aws.Config
	// PlatformManaged is true when the bucket lives in this service's account.
	PlatformManaged bool
	// AccountID is the account expected to own the bucket. Pass it as
	// ExpectedBucketOwner on every S3 call so a drifted record fails the call
	// rather than acting on the wrong bucket.
	AccountID string
	// RoleName is the cross-account role to assume, empty when there is none.
	//
	// CONTRACT for the storage node provisioner (not yet built): a task
	// receives ACCOUNT_ID always, and ROLE_NAME only when there is a role to
	// assume. An EMPTY ROLE_NAME means the bucket is in the task's own
	// account and the task must act with its own credentials rather than
	// assuming into itself. Requiring ROLE_NAME unconditionally would make
	// platform-owned storage nodes unserviceable.
	RoleName string
}

// Owner is the ExpectedBucketOwner to send with S3 calls on this bucket.
func (a BucketAccess) Owner() *string { return aws.String(a.AccountID) }

// ResolveBucketAccess decides how to reach a storage node's bucket.
//
// An account row naming this account marks a platform-owned bucket; one
// naming another account must carry a role to assume, and a row that names
// another account with no role is refused rather than guessed at.
func ResolveBucketAccess(ctx context.Context, cfg aws.Config, account store_dynamodb.Account,
	node models.DynamoDBStorageNode) (BucketAccess, error) {

	platform, err := PlatformAccountID(ctx, cfg)
	if err != nil {
		return BucketAccess{}, err
	}
	return ResolveBucketAccessWithPlatform(ctx, cfg, platform, account, node)
}

// ResolveBucketAccessWithPlatform is ResolveBucketAccess for a caller that
// already knows which account it is running in.
func ResolveBucketAccessWithPlatform(ctx context.Context, cfg aws.Config, platform string,
	account store_dynamodb.Account, node models.DynamoDBStorageNode) (BucketAccess, error) {

	if account.AccountId == platform || (account.AccountId == "" && account.RoleName == "") {
		if account.RoleName != "" {
			log.Printf("storage node %s: account row %s names this account (%s) but also a role "+
				"(%s) — ignoring the role; a bucket in our own account is not reached by assuming "+
				"into ourselves", node.Uuid, account.Uuid, platform, account.RoleName)
		}
		return BucketAccess{
			Config:          regionalCopy(cfg, node),
			PlatformManaged: true,
			AccountID:       platform,
		}, nil
	}

	if account.RoleName == "" {
		return BucketAccess{}, fmt.Errorf(
			"storage node %s: account row %s names account %s but carries no role to assume",
			node.Uuid, account.Uuid, account.AccountId)
	}

	// The role's account is also the account asserted as the bucket's owner.
	// If those ever diverge — a bucket owned by one account reached through a
	// role in another — the call fails closed, which is the outcome we want
	// until someone has decided what that arrangement should mean.
	roleArn := fmt.Sprintf("arn:aws:iam::%s:role/%s", account.AccountId, account.RoleName)
	crossAccount, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(BucketRegion(cfg, node)),
		config.WithCredentialsProvider(
			stscreds.NewAssumeRoleProvider(sts.NewFromConfig(cfg), roleArn),
		),
	)
	if err != nil {
		return BucketAccess{}, fmt.Errorf("assuming %s: %w", roleArn, err)
	}

	return BucketAccess{
		Config:    crossAccount,
		AccountID: account.AccountId,
		RoleName:  account.RoleName,
	}, nil
}

// BucketRegion is the region whose S3 endpoint can answer about this bucket.
// The platform's Africa bucket lives in af-south-1 while the service runs in
// us-east-1, so the node's own region wins whenever it has one.
func BucketRegion(cfg aws.Config, node models.DynamoDBStorageNode) string {
	if node.Region != "" {
		return node.Region
	}
	return cfg.Region
}

func regionalCopy(cfg aws.Config, node models.DynamoDBStorageNode) aws.Config {
	c := cfg.Copy()
	c.Region = BucketRegion(cfg, node)
	return c
}
