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

const appStorePolicySid = "AllowCrossAccountPull"

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
}

// PolicyPrincipal represents the principal block in a policy statement.
// AWS may return the AWS field as a single string or as an array of strings,
// so we use a custom unmarshaler to handle both forms.
type PolicyPrincipal struct {
	AWS []string `json:"AWS"`
}

func (p *PolicyPrincipal) UnmarshalJSON(data []byte) error {
	var raw struct {
		AWS json.RawMessage `json:"AWS"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw.AWS) == 0 {
		p.AWS = []string{}
		return nil
	}
	// Try array first
	if raw.AWS[0] == '[' {
		return json.Unmarshal(raw.AWS, &p.AWS)
	}
	// Single string
	var single string
	if err := json.Unmarshal(raw.AWS, &single); err != nil {
		return err
	}
	p.AWS = []string{single}
	return nil
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
// on the repository policy. It is a no-op if the account already has access.
// It retries if a concurrent update overwrites the principal (read-modify-write race).
func (p *appStorePolicy) EnsureAccess(ctx context.Context, accountId string) error {
	principal := fmt.Sprintf("arn:aws:iam::%s:root", accountId)

	for attempt := 0; attempt <= maxRetries; attempt++ {
		policy, err := p.getPolicy(ctx)
		if err != nil {
			return fmt.Errorf("error getting ECR repository policy: %w", err)
		}

		if ContainsPrincipal(policy, principal) {
			return nil
		}

		AddPrincipal(&policy, principal)

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

		// Re-read the policy to verify our principal survived (another writer may have overwritten it).
		policy, err = p.getPolicy(ctx)
		if err != nil {
			return fmt.Errorf("error verifying ECR repository policy: %w", err)
		}

		if ContainsPrincipal(policy, principal) {
			return nil
		}
	}

	return fmt.Errorf("failed to add principal %s to ECR policy after %d retries: concurrent modification", principal, maxRetries)
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
