// Package roleaccesstest stands in for STS in handler tests: the role check
// answers what a test sets, and never calls AWS.
package roleaccesstest

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
	"github.com/pennsieve/account-service/internal/roleaccess"
)

type assumer struct{ err error }

func (a assumer) AssumeRole(context.Context, *sts.AssumeRoleInput, ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	if a.err != nil {
		return nil, a.err
	}
	return &sts.AssumeRoleOutput{}, nil
}

// Answer makes the role check answer with AssumeRole error code code ("" for
// a role that works) until the returned function restores the previous one.
func Answer(code string) (restore func()) {
	prev := roleaccess.NewAssumer
	var err error
	if code != "" {
		err = &smithy.GenericAPIError{Code: code, Message: code}
	}
	roleaccess.NewAssumer = func(aws.Config) roleaccess.Assumer { return assumer{err: err} }
	return func() { roleaccess.NewAssumer = prev }
}
