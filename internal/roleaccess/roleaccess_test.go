package roleaccess

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
)

// fakeAssumer answers every AssumeRole with err, recording the input.
type fakeAssumer struct {
	err error
	in  *sts.AssumeRoleInput
}

func (f *fakeAssumer) AssumeRole(_ context.Context, in *sts.AssumeRoleInput, _ ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	f.in = in
	if f.err != nil {
		return nil, f.err
	}
	return &sts.AssumeRoleOutput{}, nil
}

func apiErr(code string) error {
	return &smithy.GenericAPIError{Code: code, Message: code}
}

// A refusal means the role is gone or doesn't trust Pennsieve; anything else
// isn't an answer about the role, and the caller acts as if it still works.
func TestCheck(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want Usability
	}{
		{"assumed", nil, Usable},
		{"access denied (role gone or trust changed)", apiErr("AccessDenied"), Unusable},
		{"no such entity", apiErr("NoSuchEntity"), Unusable},
		{"throttled", apiErr("Throttling"), Unknown},
		{"AWS-side error", apiErr("InternalFailure"), Unknown},
		{"network", &net.OpError{Op: "dial", Err: errors.New("connection refused")}, Unknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := Check(context.Background(), &fakeAssumer{err: c.err}, "123456789012", "Pennsieve-Compute-dev-1a2b3c4d", "")
			if got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

// The account's external ID is sent when it has one; older connections have
// none and are assumed without it.
func TestCheckExternalId(t *testing.T) {
	f := &fakeAssumer{}
	Check(context.Background(), f, "123456789012", "r", "pn-0123456789abcdef0123456789abcdef")
	if f.in.ExternalId == nil || *f.in.ExternalId != "pn-0123456789abcdef0123456789abcdef" {
		t.Fatalf("external id %v", f.in.ExternalId)
	}
	if *f.in.RoleArn != "arn:aws:iam::123456789012:role/r" {
		t.Fatalf("role arn %s", *f.in.RoleArn)
	}
	Check(context.Background(), f, "123456789012", "r", "")
	if f.in.ExternalId != nil {
		t.Fatal("no external id for an older connection")
	}
}

// Without a role to check there's no answer.
func TestCheckWithoutRole(t *testing.T) {
	if got, err := Check(context.Background(), &fakeAssumer{}, "123456789012", "", ""); got != Unknown || err == nil {
		t.Fatalf("got %v, %v", got, err)
	}
}
