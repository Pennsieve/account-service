package ecr

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrTypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
)

const (
	appStorePolicySid       = "AllowCrossAccountPull"
	appStoreLambdaPolicySid = "AllowLambdaCrossAccountPull"
	lambdaServicePrincipal  = "lambda.amazonaws.com"
)

// PolicyDocument represents an ECR repository policy.
type PolicyDocument struct {
	Version   string            `json:"Version"`
	Statement []PolicyStatement `json:"Statement"`
}

// PolicyStatement represents a single statement in an ECR repository policy.
type PolicyStatement struct {
	Sid       string          `json:"Sid"`
	Effect    string          `json:"Effect"`
	Principal PolicyPrincipal `json:"Principal"`
	Action    []string        `json:"Action"`
	Condition PolicyCondition `json:"Condition,omitempty"`
}

// PolicyCondition models the optional Condition block of a policy statement.
// Outer key is the operator (e.g. "StringLike"); inner key is the condition
// key (e.g. "aws:sourceArn"); value is a string-or-array list.
type PolicyCondition map[string]map[string]ConditionValues

// ConditionValues holds a string-or-array policy condition value.
type ConditionValues []string

func (c *ConditionValues) UnmarshalJSON(data []byte) error {
	vals, err := decodeStringOrArray(data)
	if err != nil {
		return err
	}
	*c = vals
	return nil
}

// PolicyPrincipal represents the principal block in a policy statement.
// AWS may return AWS/Service fields as a single string or array of strings,
// so we use a custom unmarshaler to handle both forms.
type PolicyPrincipal struct {
	AWS     []string `json:"AWS,omitempty"`
	Service []string `json:"Service,omitempty"`
}

func (p *PolicyPrincipal) UnmarshalJSON(data []byte) error {
	var raw struct {
		AWS     json.RawMessage `json:"AWS"`
		Service json.RawMessage `json:"Service"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	awsVals, err := decodeStringOrArray(raw.AWS)
	if err != nil {
		return err
	}
	svcVals, err := decodeStringOrArray(raw.Service)
	if err != nil {
		return err
	}
	if awsVals == nil {
		awsVals = []string{}
	}
	p.AWS = awsVals
	p.Service = svcVals
	return nil
}

func decodeStringOrArray(data json.RawMessage) ([]string, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if data[0] == '[' {
		var arr []string
		if err := json.Unmarshal(data, &arr); err != nil {
			return nil, err
		}
		return arr, nil
	}
	var single string
	if err := json.Unmarshal(data, &single); err != nil {
		return nil, err
	}
	return []string{single}, nil
}

// AppStorePolicy manages cross-account pull access on an ECR repository.
type AppStorePolicy interface {
	EnsureAccess(ctx context.Context, accountId string) error
}

type appStorePolicy struct {
	client   *ecr.Client
	repoName string
}

// NewAppStorePolicy creates an AppStorePolicy backed by the given ECR client and repository name.
func NewAppStorePolicy(client *ecr.Client, repoName string) AppStorePolicy {
	return &appStorePolicy{client: client, repoName: repoName}
}

// maxRetries is the number of times EnsureAccess will retry after detecting
// that a concurrent write overwrote our principal.
const maxRetries = 3

// EnsureAccess adds the given AWS account as a cross-account pull principal
// on the repository policy and grants the Lambda service principal permission
// to pull images for Lambda functions in that account. It is a no-op if both
// entries are already present, and retries if a concurrent update overwrites
// them (read-modify-write race).
//
// Lambda's CreateFunction/UpdateFunctionCode pulls the image using the
// lambda.amazonaws.com service principal, not the caller's IAM role, so the
// account-root principal alone is not sufficient for cross-account image
// references. See: https://docs.aws.amazon.com/lambda/latest/dg/images-create.html#images-permissions
func (p *appStorePolicy) EnsureAccess(ctx context.Context, accountId string) error {
	principal := fmt.Sprintf("arn:aws:iam::%s:root", accountId)
	sourceArn := LambdaSourceArn(accountId)

	for attempt := 0; attempt <= maxRetries; attempt++ {
		policy, err := p.getPolicy(ctx)
		if err != nil {
			return fmt.Errorf("error getting ECR repository policy: %w", err)
		}

		if ContainsPrincipal(policy, principal) && ContainsLambdaSourceArn(policy, sourceArn) {
			return nil
		}
		if !ContainsPrincipal(policy, principal) {
			AddPrincipal(&policy, principal)
		}
		if !ContainsLambdaSourceArn(policy, sourceArn) {
			AddLambdaSourceArn(&policy, sourceArn)
		}

		policyJSON, err := json.Marshal(policy)
		if err != nil {
			return fmt.Errorf("error marshaling ECR policy: %w", err)
		}

		_, err = p.client.SetRepositoryPolicy(ctx, &ecr.SetRepositoryPolicyInput{
			RepositoryName: aws.String(p.repoName),
			PolicyText:     aws.String(string(policyJSON)),
		})
		if err != nil {
			return fmt.Errorf("error setting ECR repository policy: %w", err)
		}

		// Re-read the policy to verify our entries survived (another writer may have overwritten them).
		policy, err = p.getPolicy(ctx)
		if err != nil {
			return fmt.Errorf("error verifying ECR repository policy: %w", err)
		}

		if ContainsPrincipal(policy, principal) && ContainsLambdaSourceArn(policy, sourceArn) {
			return nil
		}
	}

	return fmt.Errorf("failed to add principal %s or lambda source arn %s to ECR policy after %d retries: concurrent modification", principal, sourceArn, maxRetries)
}

func (p *appStorePolicy) getPolicy(ctx context.Context) (PolicyDocument, error) {
	resp, err := p.client.GetRepositoryPolicy(ctx, &ecr.GetRepositoryPolicyInput{
		RepositoryName: aws.String(p.repoName),
	})
	if err != nil {
		var rnfe *ecrTypes.RepositoryPolicyNotFoundException
		if stderrors.As(err, &rnfe) {
			return NewPullPolicy(nil), nil
		}
		return PolicyDocument{}, err
	}

	var policy PolicyDocument
	if err := json.Unmarshal([]byte(*resp.PolicyText), &policy); err != nil {
		return PolicyDocument{}, fmt.Errorf("error parsing ECR policy: %w", err)
	}
	return policy, nil
}

// NewPullPolicy returns a policy document with a single cross-account pull statement.
func NewPullPolicy(principals []string) PolicyDocument {
	if principals == nil {
		principals = []string{}
	}
	return PolicyDocument{
		Version: "2012-10-17",
		Statement: []PolicyStatement{
			{
				Sid:    appStorePolicySid,
				Effect: "Allow",
				Principal: PolicyPrincipal{
					AWS: principals,
				},
				Action: []string{
					"ecr:GetDownloadUrlForLayer",
					"ecr:BatchGetImage",
					"ecr:BatchCheckLayerAvailability",
				},
			},
		},
	}
}

// ContainsPrincipal reports whether the policy already includes the given principal ARN.
func ContainsPrincipal(policy PolicyDocument, principal string) bool {
	for _, stmt := range policy.Statement {
		if stmt.Sid == appStorePolicySid {
			for _, p := range stmt.Principal.AWS {
				if p == principal {
					return true
				}
			}
		}
	}
	return false
}

// AddPrincipal appends a principal ARN to the cross-account pull statement.
// If the statement does not exist, a new one is created.
func AddPrincipal(policy *PolicyDocument, principal string) {
	for i, stmt := range policy.Statement {
		if stmt.Sid == appStorePolicySid {
			policy.Statement[i].Principal.AWS = append(stmt.Principal.AWS, principal)
			return
		}
	}
	policy.Statement = append(policy.Statement, PolicyStatement{
		Sid:    appStorePolicySid,
		Effect: "Allow",
		Principal: PolicyPrincipal{
			AWS: []string{principal},
		},
		Action: []string{
			"ecr:GetDownloadUrlForLayer",
			"ecr:BatchGetImage",
			"ecr:BatchCheckLayerAvailability",
		},
	})
}

// LambdaSourceArn returns the aws:sourceArn pattern used to scope Lambda
// service-principal access to Lambda functions in the given account.
func LambdaSourceArn(accountId string) string {
	return fmt.Sprintf("arn:aws:lambda:*:%s:function:*", accountId)
}

// ContainsLambdaSourceArn reports whether the policy's Lambda service-principal
// statement already includes the given aws:sourceArn entry.
func ContainsLambdaSourceArn(policy PolicyDocument, sourceArn string) bool {
	for _, stmt := range policy.Statement {
		if stmt.Sid != appStoreLambdaPolicySid {
			continue
		}
		for _, arn := range stmt.Condition["StringLike"]["aws:sourceArn"] {
			if arn == sourceArn {
				return true
			}
		}
	}
	return false
}

// AddLambdaSourceArn appends a sourceArn pattern to the Lambda service-principal
// statement's StringLike/aws:sourceArn condition. If the statement does not
// exist, a new one is created.
func AddLambdaSourceArn(policy *PolicyDocument, sourceArn string) {
	for i, stmt := range policy.Statement {
		if stmt.Sid != appStoreLambdaPolicySid {
			continue
		}
		if policy.Statement[i].Condition == nil {
			policy.Statement[i].Condition = PolicyCondition{}
		}
		if policy.Statement[i].Condition["StringLike"] == nil {
			policy.Statement[i].Condition["StringLike"] = map[string]ConditionValues{}
		}
		policy.Statement[i].Condition["StringLike"]["aws:sourceArn"] = append(
			policy.Statement[i].Condition["StringLike"]["aws:sourceArn"], sourceArn,
		)
		return
	}
	policy.Statement = append(policy.Statement, PolicyStatement{
		Sid:    appStoreLambdaPolicySid,
		Effect: "Allow",
		Principal: PolicyPrincipal{
			Service: []string{lambdaServicePrincipal},
		},
		Action: []string{
			"ecr:GetDownloadUrlForLayer",
			"ecr:BatchGetImage",
		},
		Condition: PolicyCondition{
			"StringLike": {
				"aws:sourceArn": ConditionValues{sourceArn},
			},
		},
	})
}
