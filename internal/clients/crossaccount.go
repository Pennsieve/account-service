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
