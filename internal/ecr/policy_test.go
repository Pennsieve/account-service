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

func TestLambdaSourceArn(t *testing.T) {
	assert.Equal(t, "arn:aws:lambda:*:111111111111:function:*", LambdaSourceArn("111111111111"))
}

func TestContainsLambdaSourceArn(t *testing.T) {
	var policy PolicyDocument
	AddLambdaSourceArn(&policy, LambdaSourceArn("111111111111"))
	AddLambdaSourceArn(&policy, LambdaSourceArn("222222222222"))

	assert.True(t, ContainsLambdaSourceArn(policy, LambdaSourceArn("111111111111")))
	assert.True(t, ContainsLambdaSourceArn(policy, LambdaSourceArn("222222222222")))
	assert.False(t, ContainsLambdaSourceArn(policy, LambdaSourceArn("333333333333")))
}

func TestContainsLambdaSourceArn_EmptyPolicy(t *testing.T) {
	var policy PolicyDocument
	assert.False(t, ContainsLambdaSourceArn(policy, LambdaSourceArn("111111111111")))
}

func TestAddLambdaSourceArn_CreatesStatementIfMissing(t *testing.T) {
	var policy PolicyDocument

	AddLambdaSourceArn(&policy, LambdaSourceArn("111111111111"))

	assert.Len(t, policy.Statement, 1)
	stmt := policy.Statement[0]
	assert.Equal(t, appStoreLambdaPolicySid, stmt.Sid)
	assert.Equal(t, "Allow", stmt.Effect)
	assert.Equal(t, []string{lambdaServicePrincipal}, stmt.Principal.Service)
	assert.Empty(t, stmt.Principal.AWS)
	assert.Equal(t,
		ConditionValues{"arn:aws:lambda:*:111111111111:function:*"},
		stmt.Condition["StringLike"]["aws:sourceArn"],
	)
}

func TestAddLambdaSourceArn_AppendsToExistingStatement(t *testing.T) {
	var policy PolicyDocument
	AddLambdaSourceArn(&policy, LambdaSourceArn("111111111111"))
	AddLambdaSourceArn(&policy, LambdaSourceArn("222222222222"))

	assert.Len(t, policy.Statement, 1)
	assert.Equal(t,
		ConditionValues{
			"arn:aws:lambda:*:111111111111:function:*",
			"arn:aws:lambda:*:222222222222:function:*",
		},
		policy.Statement[0].Condition["StringLike"]["aws:sourceArn"],
	)
}

func TestAddLambdaSourceArn_CoexistsWithAWSPrincipalStatement(t *testing.T) {
	policy := NewPullPolicy([]string{"arn:aws:iam::111111111111:root"})

	AddLambdaSourceArn(&policy, LambdaSourceArn("111111111111"))

	assert.Len(t, policy.Statement, 2)
	assert.Equal(t, appStorePolicySid, policy.Statement[0].Sid)
	assert.Equal(t, appStoreLambdaPolicySid, policy.Statement[1].Sid)
}

func TestPolicy_RoundtripWithLambdaStatement(t *testing.T) {
	original := NewPullPolicy([]string{"arn:aws:iam::111111111111:root"})
	AddLambdaSourceArn(&original, LambdaSourceArn("111111111111"))

	data, err := json.Marshal(original)
	assert.NoError(t, err)

	var got PolicyDocument
	assert.NoError(t, json.Unmarshal(data, &got))

	assert.True(t, ContainsPrincipal(got, "arn:aws:iam::111111111111:root"))
	assert.True(t, ContainsLambdaSourceArn(got, LambdaSourceArn("111111111111")))

	// The Lambda statement's Principal must serialize as Service-only (no empty AWS field).
	assert.NotContains(t, string(data), `"AWS":[]`)
	assert.Contains(t, string(data), `"Service":["lambda.amazonaws.com"]`)
}

func TestPolicyPrincipal_UnmarshalJSON_ServiceSingleString(t *testing.T) {
	policyJSON := `{
		"Version": "2012-10-17",
		"Statement": [{
			"Sid": "AllowLambdaCrossAccountPull",
			"Effect": "Allow",
			"Principal": {"Service": "lambda.amazonaws.com"},
			"Action": ["ecr:BatchGetImage"],
			"Condition": {"StringLike": {"aws:sourceArn": "arn:aws:lambda:*:111111111111:function:*"}}
		}]
	}`

	var policy PolicyDocument
	err := json.Unmarshal([]byte(policyJSON), &policy)
	assert.NoError(t, err)
	assert.Equal(t, []string{"lambda.amazonaws.com"}, policy.Statement[0].Principal.Service)
	assert.Equal(t,
		ConditionValues{"arn:aws:lambda:*:111111111111:function:*"},
		policy.Statement[0].Condition["StringLike"]["aws:sourceArn"],
	)
}

func TestPolicyPrincipal_UnmarshalJSON_ServiceArray(t *testing.T) {
	policyJSON := `{
		"Version": "2012-10-17",
		"Statement": [{
			"Sid": "AllowLambdaCrossAccountPull",
			"Effect": "Allow",
			"Principal": {"Service": ["lambda.amazonaws.com"]},
			"Action": ["ecr:BatchGetImage"],
			"Condition": {"StringLike": {"aws:sourceArn": ["arn:aws:lambda:*:111111111111:function:*", "arn:aws:lambda:*:222222222222:function:*"]}}
		}]
	}`

	var policy PolicyDocument
	err := json.Unmarshal([]byte(policyJSON), &policy)
	assert.NoError(t, err)
	assert.Equal(t, []string{"lambda.amazonaws.com"}, policy.Statement[0].Principal.Service)
	assert.Equal(t,
		ConditionValues{
			"arn:aws:lambda:*:111111111111:function:*",
			"arn:aws:lambda:*:222222222222:function:*",
		},
		policy.Statement[0].Condition["StringLike"]["aws:sourceArn"],
	)
}
