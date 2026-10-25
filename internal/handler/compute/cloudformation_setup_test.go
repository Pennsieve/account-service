package compute

import (
	"net/url"
	"strings"
	"testing"
)

const testTemplateURL = "https://pennsieve-dev-cloudformation-templates-use1.s3.us-east-1.amazonaws.com/cfn/pennsieve-compute-role/v3.yaml"

func testSetupResponse() roleConfigResponse {
	return roleConfigResponse{
		RoleName:           "Pennsieve-Compute-dev-1a2b3c4d",
		ExternalId:         "pn-0123456789abcdef0123456789abcdef",
		PennsieveAccountId: "123456789012",
	}
}

func TestCloudFormationSetup_QuickCreateUrl(t *testing.T) {
	cf := newCloudFormationSetup(testTemplateURL, "3", "us-east-1", testSetupResponse())
	if cf == nil {
		t.Fatal("want a CloudFormation setup")
	}
	if cf.TemplateUrl != testTemplateURL || cf.TemplateVersion != "3" {
		t.Fatalf("template %q v%q", cf.TemplateUrl, cf.TemplateVersion)
	}
	prefix := "https://console.aws.amazon.com/cloudformation/home?region=us-east-1#/stacks/quickcreate?"
	if !strings.HasPrefix(cf.QuickCreateUrl, prefix) {
		t.Fatalf("quick-create URL %q", cf.QuickCreateUrl)
	}
	// The part after the fragment's "?" is a query the console reads.
	q, err := url.ParseQuery(strings.TrimPrefix(cf.QuickCreateUrl, prefix))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"templateURL":              testTemplateURL,
		"stackName":                "Pennsieve-Compute-dev-1a2b3c4d",
		"param_RoleName":           "Pennsieve-Compute-dev-1a2b3c4d",
		"param_ExternalId":         "pn-0123456789abcdef0123456789abcdef",
		"param_PennsieveAccountId": "123456789012",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	// The template URL is encoded, so its own "/" and ":" don't end the fragment's query.
	if !strings.Contains(cf.QuickCreateUrl, "templateURL=https%3A%2F%2Fpennsieve-dev") {
		t.Errorf("template URL not encoded: %q", cf.QuickCreateUrl)
	}
}

func TestCloudFormationSetup_CloudShellCommand(t *testing.T) {
	cf := newCloudFormationSetup(testTemplateURL, "3", "us-east-1", testSetupResponse())
	want := "aws cloudformation create-stack --region us-east-1 --stack-name Pennsieve-Compute-dev-1a2b3c4d" +
		" --template-url '" + testTemplateURL + "' --parameters" +
		" ParameterKey=RoleName,ParameterValue=Pennsieve-Compute-dev-1a2b3c4d" +
		" ParameterKey=ExternalId,ParameterValue=pn-0123456789abcdef0123456789abcdef" +
		" ParameterKey=PennsieveAccountId,ParameterValue=123456789012" +
		" --capabilities CAPABILITY_NAMED_IAM"
	if cf.CloudShellCommand != want {
		t.Fatalf("command\n %s\nwant\n %s", cf.CloudShellCommand, want)
	}
}

// No template published, or a legacy role setup (no external ID): no CloudFormation.
func TestCloudFormationSetup_LeftOut(t *testing.T) {
	if newCloudFormationSetup("", "3", "us-east-1", testSetupResponse()) != nil {
		t.Error("without CFN_TEMPLATE_URL there should be no CloudFormation setup")
	}
	legacy := testSetupResponse()
	legacy.ExternalId = ""
	if newCloudFormationSetup(testTemplateURL, "3", "us-east-1", legacy) != nil {
		t.Error("a role setup without an external ID can't use the template")
	}
}

// With an external ID the response names the account the role trusts; the
// legacy response is unchanged.
func TestBuildRoleConfig_PennsieveAccountId(t *testing.T) {
	withId, err := buildRoleConfig("r", "123456789012", true, func() (string, error) { return "pn-0123456789abcdef0123456789abcdef", nil })
	if err != nil || withId.PennsieveAccountId != "123456789012" {
		t.Fatalf("got %q, %v", withId.PennsieveAccountId, err)
	}
	legacy, _ := buildRoleConfig("r", "123456789012", false, nil)
	if legacy.PennsieveAccountId != "" || legacy.CloudFormation != nil {
		t.Fatal("a legacy role setup gains no fields")
	}
}
