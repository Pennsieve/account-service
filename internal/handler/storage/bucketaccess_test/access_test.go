package bucketaccess_test

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/pennsieve/account-service/internal/handler/storage"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Which path a storage node takes comes from its account row, and the row is
// hand-maintained: in prod, both platform-owned nodes name the DEV platform
// account. So the cases that matter are the two legitimate shapes, the
// corrupt shape in between, and the ExpectedBucketOwner that every S3 call
// carries so a drifted row fails the call instead of acting on the wrong
// bucket.

const platform = "740463337177"

func node(bucket, region string) models.DynamoDBStorageNode {
	return models.DynamoDBStorageNode{Uuid: "node-1", StorageLocation: bucket, Region: region}
}

// PlatformAccountID memoises per process, so every test here sees the same
// account — which is also why it is passed explicitly rather than discovered.
func cfg() aws.Config {
	return aws.Config{Region: "us-east-1"}
}

func TestPlatformOwnedRowTakesThePlatformPath(t *testing.T) {
	// The shape a platform-owned storage node's row has once corrected:
	// this account, and no role, because there is nothing to assume.
	account := store_dynamodb.Account{Uuid: "acct-1", AccountId: platform, RoleName: ""}

	access, err := storage.ResolveBucketAccessWithPlatform(context.Background(), cfg(), platform,
		account, node("pennsieve-prod-storage-use1", "us-east-1"))
	require.NoError(t, err)

	assert.True(t, access.PlatformManaged)
	assert.Equal(t, platform, access.AccountID)
	assert.Empty(t, access.RoleName, "a platform bucket hands the provisioner no role to assume")
}

func TestCrossAccountRowCarriesTheRole(t *testing.T) {
	account := store_dynamodb.Account{Uuid: "acct-2", AccountId: "225366564863",
		RoleName: "Pennsieve-Compute-prod-072e89ee"}

	access, err := storage.ResolveBucketAccessWithPlatform(context.Background(), cfg(), platform,
		account, node("prod-rejoin-storage-use1", "us-east-1"))
	require.NoError(t, err)

	assert.False(t, access.PlatformManaged)
	assert.Equal(t, "225366564863", access.AccountID)
	assert.Equal(t, "Pennsieve-Compute-prod-072e89ee", access.RoleName)
}

func TestRowNamingAnotherAccountWithNoRoleIsRefused(t *testing.T) {
	// The corrupt middle: not ours, and nothing to assume. Guessing either
	// way would act with the wrong credentials, so it is an error.
	account := store_dynamodb.Account{Uuid: "acct-3", AccountId: "225366564863", RoleName: ""}

	_, err := storage.ResolveBucketAccessWithPlatform(context.Background(), cfg(), platform,
		account, node("prod-rejoin-storage-use1", "us-east-1"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no role to assume")
}

func TestOwnerIsSentAsExpectedBucketOwner(t *testing.T) {
	// The guard itself. Whatever path was taken, the account the row claims
	// is asserted on every S3 call — verified against the live API: the right
	// owner is allowed, a stale one is AccessDenied.
	for _, tc := range []struct {
		name    string
		account store_dynamodb.Account
		want    string
	}{
		{"platform", store_dynamodb.Account{AccountId: platform}, platform},
		{"cross-account", store_dynamodb.Account{AccountId: "376308453966",
			RoleName: "Pennsieve-Compute-prod-841c0d09"}, "376308453966"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			access, err := storage.ResolveBucketAccessWithPlatform(context.Background(), cfg(),
				platform, tc.account, node("some-bucket", "us-east-1"))
			require.NoError(t, err)
			require.NotNil(t, access.Owner())
			assert.Equal(t, tc.want, *access.Owner())
		})
	}
}

func TestBucketRegionPrefersTheNodeRegion(t *testing.T) {
	// The Africa bucket is in af-south-1 while the service runs in us-east-1;
	// asking the wrong region's endpoint answers about the wrong thing.
	assert.Equal(t, "af-south-1",
		storage.BucketRegion(cfg(), node("pennsieve-prod-storage-afs1", "af-south-1")))
	assert.Equal(t, "us-east-1",
		storage.BucketRegion(cfg(), node("pennsieve-prod-storage-use1", "")))
}
