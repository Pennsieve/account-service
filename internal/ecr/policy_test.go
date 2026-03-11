package ecr

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewPullPolicy(t *testing.T) {
	policy := NewPullPolicy([]string{"arn:aws:iam::111111111111:root"})

	assert.Equal(t, "2012-10-17", policy.Version)
	assert.Len(t, policy.Statement, 1)
	assert.Equal(t, appStorePolicySid, policy.Statement[0].Sid)
	assert.Equal(t, []string{"arn:aws:iam::111111111111:root"}, policy.Statement[0].Principal.AWS)
}

func TestNewPullPolicy_NilPrincipals(t *testing.T) {
	policy := NewPullPolicy(nil)

	assert.Len(t, policy.Statement, 1)
	assert.Empty(t, policy.Statement[0].Principal.AWS)
}

func TestContainsPrincipal(t *testing.T) {
	policy := NewPullPolicy([]string{
		"arn:aws:iam::111111111111:root",
		"arn:aws:iam::222222222222:root",
	})

	assert.True(t, ContainsPrincipal(policy, "arn:aws:iam::111111111111:root"))
	assert.True(t, ContainsPrincipal(policy, "arn:aws:iam::222222222222:root"))
	assert.False(t, ContainsPrincipal(policy, "arn:aws:iam::333333333333:root"))
}

func TestContainsPrincipal_EmptyPolicy(t *testing.T) {
	policy := NewPullPolicy(nil)
	assert.False(t, ContainsPrincipal(policy, "arn:aws:iam::111111111111:root"))
}

func TestAddPrincipal_AppendsToExistingStatement(t *testing.T) {
	policy := NewPullPolicy([]string{"arn:aws:iam::111111111111:root"})

	AddPrincipal(&policy, "arn:aws:iam::222222222222:root")

	assert.Len(t, policy.Statement, 1)
	assert.Equal(t, []string{
		"arn:aws:iam::111111111111:root",
		"arn:aws:iam::222222222222:root",
	}, policy.Statement[0].Principal.AWS)
}

func TestPolicyPrincipal_UnmarshalJSON_SingleString(t *testing.T) {
	policyJSON := `{
		"Version": "2012-10-17",
		"Statement": [{
			"Sid": "AllowCrossAccountPull",
			"Effect": "Allow",
			"Principal": {"AWS": "arn:aws:iam::111111111111:root"},
			"Action": ["ecr:GetDownloadUrlForLayer"]
		}]
	}`

	var policy PolicyDocument
	err := json.Unmarshal([]byte(policyJSON), &policy)
	assert.NoError(t, err)
	assert.Equal(t, []string{"arn:aws:iam::111111111111:root"}, policy.Statement[0].Principal.AWS)
}

func TestPolicyPrincipal_UnmarshalJSON_Array(t *testing.T) {
	policyJSON := `{
		"Version": "2012-10-17",
		"Statement": [{
			"Sid": "AllowCrossAccountPull",
			"Effect": "Allow",
			"Principal": {"AWS": ["arn:aws:iam::111111111111:root", "arn:aws:iam::222222222222:root"]},
			"Action": ["ecr:GetDownloadUrlForLayer"]
		}]
	}`

	var policy PolicyDocument
	err := json.Unmarshal([]byte(policyJSON), &policy)
	assert.NoError(t, err)
	assert.Equal(t, []string{
		"arn:aws:iam::111111111111:root",
		"arn:aws:iam::222222222222:root",
	}, policy.Statement[0].Principal.AWS)
}

func TestAddPrincipal_CreatesStatementIfMissing(t *testing.T) {
	policy := PolicyDocument{Version: "2012-10-17"}

	AddPrincipal(&policy, "arn:aws:iam::111111111111:root")

	assert.Len(t, policy.Statement, 1)
	assert.Equal(t, appStorePolicySid, policy.Statement[0].Sid)
	assert.Equal(t, []string{"arn:aws:iam::111111111111:root"}, policy.Statement[0].Principal.AWS)
}
