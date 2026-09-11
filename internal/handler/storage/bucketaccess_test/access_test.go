package bucketaccess_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/pennsieve/account-service/internal/handler/storage"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The bug this guards: a storage node's account record can name an account
// that does not own its bucket (both platform-owned prod storage nodes do
// exactly that today). Ownership therefore has to come from S3, and the
// answers that matter are the three below — ours, not ours, and "could not
// find out", which must never be mistaken for either.

type headBucketFunc func(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error)

func (f headBucketFunc) HeadBucket(ctx context.Context, in *s3.HeadBucketInput,
	opts ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	return f(ctx, in, opts...)
}

func node(bucket, region string) models.DynamoDBStorageNode {
	return models.DynamoDBStorageNode{Uuid: "node-1", StorageLocation: bucket, Region: region}
}

func httpErr(status int) error {
	return &awshttp.ResponseError{
		ResponseError: &smithyhttp.ResponseError{
			Response: &smithyhttp.Response{Response: &http.Response{StatusCode: status}},
			Err:      fmt.Errorf("status %d", status),
		},
	}
}

func TestIsPlatformManaged_OwnedBucket(t *testing.T) {
	api := headBucketFunc(func(_ context.Context, in *s3.HeadBucketInput,
		_ ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
		// the ownership assertion must actually be sent — without it S3
		// answers for any bucket we can reach, which is the whole bug
		require.NotNil(t, in.ExpectedBucketOwner)
		assert.Equal(t, "111111111111", *in.ExpectedBucketOwner)
		return &s3.HeadBucketOutput{}, nil
	})

	ok, err := storage.IsPlatformManaged(context.Background(), api, "111111111111",
		node("pennsieve-prod-storage-use1", "us-east-1"))
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestIsPlatformManaged_ForbiddenMeansNotOurs(t *testing.T) {
	// A bucket policy can grant this account data access without transferring
	// ownership — exactly the case for the three customer buckets. 403 here
	// is an answer, not a failure.
	api := headBucketFunc(func(context.Context, *s3.HeadBucketInput,
		...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
		return nil, httpErr(http.StatusForbidden)
	})

	ok, err := storage.IsPlatformManaged(context.Background(), api, "111111111111",
		node("dev-sparc-storage-use1", "us-east-1"))
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestIsPlatformManaged_MissingBucketIsNotOurs(t *testing.T) {
	api := headBucketFunc(func(context.Context, *s3.HeadBucketInput,
		...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
		return nil, &s3types.NotFound{}
	})

	ok, err := storage.IsPlatformManaged(context.Background(), api, "111111111111",
		node("gone", "us-east-1"))
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestIsPlatformManaged_UnknownFailureIsAnError(t *testing.T) {
	// The important one. A throttle or a network fault is not evidence of
	// anything; treating it as "not ours" would send a DELETE at a
	// cross-account role, and treating it as "ours" would run it here.
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"throttled", &smithy.GenericAPIError{Code: "SlowDown", Message: "please slow down"}},
		{"server error", httpErr(http.StatusInternalServerError)},
		{"no response at all", errors.New("dial tcp: i/o timeout")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := headBucketFunc(func(context.Context, *s3.HeadBucketInput,
				...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
				return nil, tc.err
			})

			ok, err := storage.IsPlatformManaged(context.Background(), api, "111111111111",
				node("pennsieve-prod-storage-use1", "us-east-1"))
			require.Error(t, err, "an unknown failure must not be reported as an ownership answer")
			assert.False(t, ok)
		})
	}
}

func TestRegionOfPrefersTheNodeRegion(t *testing.T) {
	// The Africa bucket is in af-south-1 while the service runs in us-east-1;
	// asking the wrong region's endpoint answers about the wrong thing.
	cfg := aws.Config{Region: "us-east-1"}
	assert.Equal(t, "af-south-1", storage.BucketRegion(cfg, node("pennsieve-prod-storage-afs1", "af-south-1")))
	assert.Equal(t, "us-east-1", storage.BucketRegion(cfg, node("pennsieve-prod-storage-use1", "")))
}
