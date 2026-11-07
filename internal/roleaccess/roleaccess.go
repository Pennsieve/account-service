// Package roleaccess tells whether Pennsieve can still use an account's role:
// a customer can remove it, or the CloudFormation stack that made it, without
// Pennsieve. Pennsieve then can't tear down what it built in their account,
// and a delete that waits for that teardown would never finish.
package roleaccess

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
)

// RunbookURL is the customer guide to finding and removing what Pennsieve
// built in an AWS account, without Pennsieve.
const RunbookURL = "https://github.com/Pennsieve/account-service/blob/main/docs/disconnect-without-pennsieve.md"

// Assumer is the part of STS the check uses.
type Assumer interface {
	AssumeRole(ctx context.Context, in *sts.AssumeRoleInput, optFns ...func(*sts.Options)) (*sts.AssumeRoleOutput, error)
}

// NewAssumer builds the Assumer; tests replace it.
var NewAssumer = func(cfg aws.Config) Assumer { return sts.NewFromConfig(cfg) }

// Usability is what assuming the role showed.
type Usability int

const (
	// Unknown: the check failed for another reason (throttling, network, an
	// AWS-side error); act as if the role still works.
	Unknown Usability = iota
	// Usable: Pennsieve assumed the role.
	Usable
	// Unusable: AWS refused, because the role is gone or no longer trusts
	// Pennsieve (with its external ID).
	Unusable
)

// refusals are the AssumeRole error codes that mean the role can't be used:
// AWS answers AccessDenied both for a role that doesn't exist and for one
// whose trust policy doesn't allow Pennsieve.
var refusals = map[string]bool{
	"AccessDenied":          true,
	"AccessDeniedException": true,
	"NoSuchEntity":          true,
	"NoSuchEntityException": true,
}

// Check assumes the role roleName in AWS account accountId, with externalId
// when the account has one (older connections don't), and says whether it
// could. A role name or account ID that's missing is Unknown.
func Check(ctx context.Context, assumer Assumer, accountId, roleName, externalId string) (Usability, error) {
	if accountId == "" || roleName == "" {
		return Unknown, errors.New("no role to check")
	}
	in := &sts.AssumeRoleInput{
		RoleArn:         aws.String(fmt.Sprintf("arn:aws:iam::%s:role/%s", accountId, roleName)),
		RoleSessionName: aws.String("pennsieve-role-check"),
		DurationSeconds: aws.Int32(900),
	}
	if externalId != "" {
		in.ExternalId = aws.String(externalId)
	}
	_, err := assumer.AssumeRole(ctx, in)
	return classify(err)
}

// classify sorts an AssumeRole result into Usable, Unusable or Unknown.
func classify(err error) (Usability, error) {
	if err == nil {
		return Usable, nil
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && refusals[apiErr.ErrorCode()] {
		return Unusable, err
	}
	return Unknown, err
}

// RemovedNote is the answer to a delete or disconnect that removed Pennsieve's
// records without a teardown, because the account's role was unusable.
type RemovedNote struct {
	Message             string `json:"message"`
	ResourcesMayRemain  bool   `json:"resourcesMayRemain"`
	RunbookUrl          string `json:"runbookUrl"`
	ComputeNodesRemoved int    `json:"computeNodesRemoved,omitempty"`
	StorageNodesRemoved int    `json:"storageNodesRemoved,omitempty"`
}
