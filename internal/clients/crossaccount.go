package clients

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// AssumeComputeRoleConfig returns an aws.Config whose credentials come from assuming
// the compute node's cross-account role (account.RoleName) in the customer's AWS
// account. Requests signed with this config present an *in-account* principal.
//
// This matters for calls to the compute gateway: signing with account-service's own
// (Pennsieve) identity makes the request an external, cross-org principal, which is
// denied by customer AWS Organizations guardrails (RCPs) that block access from
// accounts outside their org — even when IAM and the gateway's resource policy allow
// it. Assuming the in-account role sidesteps that guardrail entirely.
//
// The SDK wraps the assume-role provider in a credentials cache, so the STS call
// happens once per config and credentials refresh automatically as needed.
func AssumeComputeRoleConfig(ctx context.Context, cfg aws.Config, accountId, roleName, region string) (aws.Config, error) {
	stsClient := sts.NewFromConfig(cfg)
	roleArn := fmt.Sprintf("arn:aws:iam::%s:role/%s", accountId, roleName)
	crossAccountCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(
			stscreds.NewAssumeRoleProvider(stsClient, roleArn),
		),
	)
	if err != nil {
		return aws.Config{}, fmt.Errorf("assuming compute role %s: %w", roleArn, err)
	}
	return crossAccountCfg, nil
}
