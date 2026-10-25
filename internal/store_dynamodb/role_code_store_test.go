package store_dynamodb_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func roleCodeStore() store_dynamodb.RoleCodeStore {
	return store_dynamodb.NewRoleCodeStore(test.GetTestClient(), test.TEST_ROLE_CODES_TABLE)
}

func TestRoleCodeStore_IssueGetAndUse(t *testing.T) {
	ctx := context.Background()
	codes := roleCodeStore()
	id := "pn-" + test.GenerateTestId()
	now := time.Now().Unix()
	require.NoError(t, codes.Issue(ctx, store_dynamodb.RoleCode{ExternalId: id, UserId: "u1", RoleName: "r1", IssuedAt: now, ExpiresAt: now + 3600}))
	assert.Error(t, codes.Issue(ctx, store_dynamodb.RoleCode{ExternalId: id, UserId: "u2", RoleName: "r1", ExpiresAt: now + 3600}),
		"an issued code is never replaced")

	got, found, err := codes.Get(ctx, id)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "u1", got.UserId)
	assert.False(t, got.Used)

	assert.ErrorIs(t, codes.MarkUsed(ctx, id, "u2", "r1", now), store_dynamodb.ErrRoleCodeUnavailable, "another user")
	assert.ErrorIs(t, codes.MarkUsed(ctx, id, "u1", "r2", now), store_dynamodb.ErrRoleCodeUnavailable, "another role")
	assert.ErrorIs(t, codes.MarkUsed(ctx, id, "u1", "r1", now+7200), store_dynamodb.ErrRoleCodeUnavailable, "expired")
	assert.ErrorIs(t, codes.MarkUsed(ctx, "pn-never-"+id, "u1", "r1", now), store_dynamodb.ErrRoleCodeUnavailable, "never issued")
	require.NoError(t, codes.MarkUsed(ctx, id, "u1", "r1", now))
	assert.ErrorIs(t, codes.MarkUsed(ctx, id, "u1", "r1", now), store_dynamodb.ErrRoleCodeUnavailable, "used")

	require.NoError(t, codes.Release(ctx, id))
	require.NoError(t, codes.MarkUsed(ctx, id, "u1", "r1", now), "released, it can be used again")

	_, found, err = codes.Get(ctx, "pn-never-"+id)
	require.NoError(t, err)
	assert.False(t, found)
}

// The conditional write lets exactly one of several racing registrations use a code.
func TestRoleCodeStore_MarkUsedRace(t *testing.T) {
	ctx := context.Background()
	codes := roleCodeStore()
	id := "pn-race-" + test.GenerateTestId()
	now := time.Now().Unix()
	require.NoError(t, codes.Issue(ctx, store_dynamodb.RoleCode{ExternalId: id, UserId: "u1", RoleName: "r1", ExpiresAt: now + 3600}))

	var wg sync.WaitGroup
	results := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- codes.MarkUsed(ctx, id, "u1", "r1", now)
		}()
	}
	wg.Wait()
	close(results)
	won := 0
	for err := range results {
		if err == nil {
			won++
		} else if !errors.Is(err, store_dynamodb.ErrRoleCodeUnavailable) {
			t.Fatalf("unexpected error %v", err)
		}
	}
	assert.Equal(t, 1, won)
}
