package storage

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
)

// Reaching a storage node's bucket takes one of two paths, and the storage
// node's account RECORD must not be what decides which.
//
// Some storage buckets live in a customer's AWS account and are reached by
// assuming that account's cross-account role. Others are the platform's own
// buckets, sitting in the very account this service runs in — there is no
// role to assume, and assuming one is at best a no-op.
//
// Deriving that from `account.AccountId` is what the old code did, and it is
// wrong in a way that hides: the account a storage node points at is written
// by hand and can name an account that does not own the bucket. It does
// today — in prod, both platform-owned storage nodes point at an account
// record that carries a different account's ID. The symptom is not a loud
// error but an AccessDenied inside a Fargate task nobody is watching, while
// the API has already answered 202.
//
// So ownership is asked of S3 instead of asserted from DynamoDB.
// ExpectedBucketOwner makes the question exact: S3 refuses the call unless
// the bucket really is owned by the account we name. "Platform-managed"
// becomes a fact about the bucket rather than a claim in a table, and a
// record that disagrees can no longer send an operation at the wrong
// account — the worst it can do now is be logged.

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

// BucketOwnerAPI is the one S3 call ownership needs, so the decision can be
// tested without an AWS account behind it.
type BucketOwnerAPI interface {
	HeadBucket(ctx context.Context, in *s3.HeadBucketInput, opts ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
}

// IsPlatformManaged reports whether this bucket is owned by platformAccountID,
// by asking S3 rather than by trusting the storage node's account record.
//
// A bucket the platform does not own answers 403 here even when a bucket
// policy grants us data access, because ExpectedBucketOwner is checked before
// anything else — which is precisely the discrimination we want.
func IsPlatformManaged(ctx context.Context, api BucketOwnerAPI, platformAccountID string,
	node models.DynamoDBStorageNode) (bool, error) {

	_, err := api.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket:              aws.String(node.StorageLocation),
		ExpectedBucketOwner: aws.String(platformAccountID),
	})
	if err == nil {
		return true, nil
	}
	if deniedOrMissing(err) {
		return false, nil
	}
	// A throttle or a network fault is not evidence of ownership either way,
	// and guessing here would route a destructive operation at the wrong
	// account. Report it and let the caller refuse.
	return false, fmt.Errorf("checking whether %s is platform-owned: %w", node.StorageLocation, err)
}

// deniedOrMissing distinguishes "this bucket is not ours" from "we could not
// find out". 403 is the ExpectedBucketOwner mismatch (or no access at all)
// and 404 is a bucket that is not there; both mean the platform does not own
// it. HeadBucket returns bare HTTP status codes rather than typed errors.
func deniedOrMissing(err error) bool {
	var nf *s3types.NotFound
	if errors.As(err, &nf) {
		return true
	}
	var api smithy.APIError
	if errors.As(err, &api) {
		switch api.ErrorCode() {
		case "Forbidden", "AccessDenied", "NotFound", "NoSuchBucket":
			return true
		}
	}
	var re *awshttp.ResponseError
	if errors.As(err, &re) {
		return re.HTTPStatusCode() == 403 || re.HTTPStatusCode() == 404
	}
	return false
}

// BucketAccess is how a storage node's bucket should be reached, resolved
// from the bucket itself.
type BucketAccess struct {
	// Config carries credentials that can act on the bucket: the service's
	// own for a platform-managed bucket, the assumed cross-account role
	// otherwise.
	Config aws.Config
	// PlatformManaged is true when the bucket lives in this service's account.
	PlatformManaged bool
	// AccountID and RoleName are what a provisioner task should act as, and
	// they are the verified account — not whatever the storage node's record
	// happens to claim.
	//
	// CONTRACT for the storage node provisioner (not yet built): a task
	// receives ACCOUNT_ID always, and ROLE_NAME only when there is a role to
	// assume. An EMPTY ROLE_NAME means the bucket is in the task's own
	// account and the task must act with its own credentials rather than
	// assuming into itself. Requiring ROLE_NAME unconditionally would make
	// platform-owned storage nodes unserviceable.
	AccountID string
	RoleName  string
}

// ResolveBucketAccess decides how to reach a storage node's bucket.
//
// When the storage node's account record disagrees with S3 about who owns the
// bucket, the bucket wins and the disagreement is logged: that record is a
// data bug someone needs to fix, but it must not be able to point an
// operation at the wrong AWS account while it goes unfixed.
func ResolveBucketAccess(ctx context.Context, cfg aws.Config, account store_dynamodb.Account,
	node models.DynamoDBStorageNode) (BucketAccess, error) {

	platform, err := PlatformAccountID(ctx, cfg)
	if err != nil {
		return BucketAccess{}, err
	}

	platformManaged, err := IsPlatformManaged(ctx, s3.NewFromConfig(regionalCopy(cfg, node)), platform, node)
	if err != nil {
		return BucketAccess{}, err
	}

	if platformManaged {
		if account.AccountId != "" && account.AccountId != platform {
			log.Printf("storage node %s: bucket %s is owned by this account (%s) but its account "+
				"record %s claims %s — using platform credentials; the record needs correcting",
				node.Uuid, node.StorageLocation, platform, account.Uuid, account.AccountId)
		}
		return BucketAccess{Config: regionalCopy(cfg, node), PlatformManaged: true,
			AccountID: platform}, nil
	}

	if account.AccountId == "" || account.RoleName == "" {
		return BucketAccess{}, fmt.Errorf(
			"storage node %s: bucket %s is not owned by this account and its account record has no "+
				"role to assume", node.Uuid, node.StorageLocation)
	}

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

	return BucketAccess{Config: crossAccount, AccountID: account.AccountId,
		RoleName: account.RoleName}, nil
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
