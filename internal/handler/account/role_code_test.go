package account

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/pennsieve/account-service/internal/store_dynamodb"
)

// fakeRoleCodes is a RoleCodeStore in memory, with DynamoDB's conditional
// MarkUsed.
type fakeRoleCodes struct {
	mu     sync.Mutex
	codes  map[string]store_dynamodb.RoleCode
	getErr error
}

func (f *fakeRoleCodes) Issue(_ context.Context, c store_dynamodb.RoleCode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.codes == nil {
		f.codes = map[string]store_dynamodb.RoleCode{}
	}
	f.codes[c.ExternalId] = c
	return nil
}

func (f *fakeRoleCodes) Get(_ context.Context, id string) (store_dynamodb.RoleCode, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.codes[id]
	return c, ok, f.getErr
}

func (f *fakeRoleCodes) MarkUsed(_ context.Context, id, userId, roleName string, now int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.codes[id]
	if !ok || c.UserId != userId || c.RoleName != roleName || c.ExpiresAt <= now || c.Used {
		return store_dynamodb.ErrRoleCodeUnavailable
	}
	c.Used = true
	f.codes[id] = c
	return nil
}

func (f *fakeRoleCodes) Release(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.codes[id]
	c.Used = false
	f.codes[id] = c
	return nil
}

func TestCheckIssuedCode(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	codes := &fakeRoleCodes{}
	issue := func(id, user, role string, expires time.Time, used bool) {
		_ = codes.Issue(context.Background(), store_dynamodb.RoleCode{
			ExternalId: id, UserId: user, RoleName: role, IssuedAt: now.Unix(), ExpiresAt: expires.Unix(), Used: used,
		})
	}
	day := now.Add(24 * time.Hour)
	issue("ok", "joost", "role-a", day, false)
	issue("expired", "joost", "role-a", now.Add(-time.Second), false)
	issue("used", "joost", "role-a", day, true)

	cases := []struct {
		name, id, user, role string
		want                 error
	}{
		{"issued to them, for that role", "ok", "joost", "role-a", nil},
		{"issued to another user", "ok", "mallory", "role-a", ErrRoleCodeNotIssued},
		{"for another role", "ok", "joost", "role-b", ErrRoleCodeNotIssued},
		{"never issued", "unknown", "joost", "role-a", ErrRoleCodeNotIssued},
		{"expired", "expired", "joost", "role-a", ErrRoleCodeNotIssued},
		{"already used", "used", "joost", "role-a", ErrRoleCodeNotIssued},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := checkIssuedCode(context.Background(), codes, c.id, c.user, c.role, now); !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}

	// Checking doesn't use the code up.
	if got, _, _ := codes.Get(context.Background(), "ok"); got.Used {
		t.Fatal("checking a code mustn't use it up")
	}

	// A store failure isn't reported as a refusal.
	broken := &fakeRoleCodes{getErr: errors.New("throttled")}
	if err := checkIssuedCode(context.Background(), broken, "ok", "joost", "role-a", now); err == nil || errors.Is(err, ErrRoleCodeNotIssued) {
		t.Fatalf("a store error should come back as one, got %v", err)
	}
}

// Of registrations racing on one code, exactly one uses it.
func TestUseIssuedCode_OneWinner(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	codes := &fakeRoleCodes{}
	_ = codes.Issue(context.Background(), store_dynamodb.RoleCode{
		ExternalId: "ok", UserId: "joost", RoleName: "role-a", ExpiresAt: now.Add(time.Hour).Unix(),
	})
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- useIssuedCode(context.Background(), codes, "ok", "joost", "role-a", now)
		}()
	}
	wg.Wait()
	close(results)
	won, refused := 0, 0
	for err := range results {
		switch {
		case err == nil:
			won++
		case errors.Is(err, ErrRoleCodeNotIssued):
			refused++
		default:
			t.Fatalf("unexpected error %v", err)
		}
	}
	if won != 1 || refused != 7 {
		t.Fatalf("want 1 winner and 7 refused, got %d and %d", won, refused)
	}
}
