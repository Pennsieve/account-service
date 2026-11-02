// Checks the provisioning policy against what the node provisioner's
// terraform actually creates, so a new Lambda, table, log group, role or
// bucket there fails here instead of failing a v2 account's provisioning
// with AccessDenied.
//
// The terraform comes from a checkout of compute-node-aws-provisioner-v2:
// PROVISIONER_DIR, else .provisioner in this repo (make test-ci clones it
// there), else a sibling checkout. Without one the test is skipped, unless
// REQUIRE_PROVISIONER is set, as on Jenkins.
package rolepolicy_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/pennsieve/account-service/internal/handler/compute"
	"github.com/stretchr/testify/require"
)

// A named resource terraform creates: the action that creates it, and its
// ARN given its name.
type createdResource struct {
	action string
	arn    func(name string) string
}

const (
	testRegion  = "us-east-1"
	testAccount = "123456789012"
)

func regional(service, format string) func(string) string {
	return func(name string) string {
		return "arn:aws:" + service + ":" + testRegion + ":" + testAccount + ":" + strings.Replace(format, "NAME", name, 1)
	}
}

func global(service, format string) func(string) string {
	return func(name string) string {
		return "arn:aws:" + service + "::" + testAccount + ":" + strings.Replace(format, "NAME", name, 1)
	}
}

// The resource types whose ARN carries a name the policy restricts. Types
// the policy allows on "*" (VPCs, clusters, load balancers, keys) aren't
// listed: any name passes.
var createdResources = map[string]createdResource{
	"aws_lambda_function":       {"lambda:CreateFunction", regional("lambda", "function:NAME")},
	"aws_dynamodb_table":        {"dynamodb:CreateTable", regional("dynamodb", "table/NAME")},
	"aws_cloudwatch_log_group":  {"logs:CreateLogGroup", regional("logs", "log-group:NAME")},
	"aws_iam_role":              {"iam:CreateRole", global("iam", "role/NAME")},
	"aws_iam_policy":            {"iam:CreatePolicy", global("iam", "policy/NAME")},
	"aws_iam_instance_profile":  {"iam:CreateInstanceProfile", global("iam", "instance-profile/NAME")},
	"aws_sfn_state_machine":     {"states:CreateStateMachine", regional("states", "stateMachine:NAME")},
	"aws_cloudwatch_event_rule": {"events:PutRule", regional("events", "rule/NAME")},
	"aws_ssm_parameter": {"ssm:PutParameter", func(name string) string {
		return regional("ssm", "parameter/NAME")(strings.TrimPrefix(name, "/"))
	}},
	"aws_s3_bucket": {"s3:CreateBucket", func(name string) string { return "arn:aws:s3:::" + name }},
}

var (
	resourceBlock = regexp.MustCompile(`(?m)^resource "([a-z0-9_]+)" "([A-Za-z0-9_-]+)" \{`)
	// A block's own name attribute: two spaces in, as terraform fmt writes it.
	nameAttribute = regexp.MustCompile(`(?m)^  (name|name_prefix|function_name|bucket)\s*=\s*(.+)$`)
	localValue    = regexp.MustCompile(`(?m)^\s+([a-z0-9_]+)\s*=\s*("[^"\n]*")\s*$`)
	interpolation = regexp.MustCompile(`\$\{[^}]*\}`)
)

func provisionerTerraform(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../../..")
	require.NoError(t, err)
	candidates := []string{
		os.Getenv("PROVISIONER_DIR"),
		filepath.Join(root, ".provisioner"),
		filepath.Join(root, "..", "compute-node-aws-provisioner-v2"),
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		dir := filepath.Join(c, "terraform")
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
	}
	if os.Getenv("REQUIRE_PROVISIONER") != "" {
		t.Fatalf("no provisioner checkout (tried %v)", candidates)
	}
	t.Skip("no compute-node-aws-provisioner-v2 checkout; set PROVISIONER_DIR")
	return ""
}

// A block's body: from its opening brace to the matching closing one.
func blockBody(src string, open int) string {
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[open : i+1]
			}
		}
	}
	return src[open:]
}

// resourceName is the name terraform gives a resource, with each
// interpolation standing for a concrete value ("x1"), so the policy's
// patterns are matched as they would be against a real name. ok is false
// when the name isn't a string or a string local.
func resourceName(value string, locals map[string]string) (name string, ok bool) {
	value = strings.TrimSpace(value)
	if v, isLocal := locals[strings.TrimPrefix(value, "local.")]; isLocal && strings.HasPrefix(value, "local.") {
		value = v
	}
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return "", false
	}
	return interpolation.ReplaceAllString(value[1:len(value)-1], "x1"), true
}

// matches is IAM's wildcard match: * any run of characters, ? any one.
func matches(pattern, value string) bool {
	re := "^" + strings.NewReplacer(`\*`, ".*", `\?`, ".").Replace(regexp.QuoteMeta(pattern)) + "$"
	return regexp.MustCompile(re).MatchString(value)
}

// allowed is whether the policy allows action on arn: an Allow statement
// covers both, and no unconditional Deny does.
func allowed(t *testing.T, doc policyDocument, action, arn string) bool {
	t.Helper()
	covers := func(s policyStatement) bool {
		actionMatch, resourceMatch := false, false
		for _, a := range list(t, s.Action) {
			actionMatch = actionMatch || matches(strings.ToLower(a), strings.ToLower(action))
		}
		for _, r := range list(t, s.Resource) {
			resourceMatch = resourceMatch || matches(r, arn)
		}
		return actionMatch && resourceMatch
	}
	allow := false
	for _, s := range doc.Statement {
		if s.Effect == "Deny" && len(s.Condition) == 0 && covers(s) {
			return false
		}
		allow = allow || (s.Effect == "Allow" && covers(s))
	}
	return allow
}

func TestProvisioningPolicy_CoversProvisionerResources(t *testing.T) {
	dir := provisionerTerraform(t)
	doc := parse(t, compute.RolePolicyJSON())

	var files []string
	require.NoError(t, filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if strings.HasSuffix(path, ".tf") {
			files = append(files, path)
		}
		return nil
	}))
	sort.Strings(files)
	require.NotEmpty(t, files, "no terraform under %s", dir)

	// Locals are read per module (directory): names often come from one.
	locals := map[string]map[string]string{}
	sources := map[string]string{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		require.NoError(t, err)
		sources[f] = string(b)
		module := filepath.Dir(f)
		if locals[module] == nil {
			locals[module] = map[string]string{}
		}
		for _, m := range localValue.FindAllStringSubmatch(string(b), -1) {
			locals[module][m[1]] = m[2]
		}
	}

	checked := 0
	for _, f := range files {
		src := sources[f]
		rel, _ := filepath.Rel(dir, f)
		for _, loc := range resourceBlock.FindAllStringSubmatchIndex(src, -1) {
			kind, label := src[loc[2]:loc[3]], src[loc[4]:loc[5]]
			created, known := createdResources[kind]
			if !known {
				continue
			}
			attr := nameAttribute.FindStringSubmatch(blockBody(src, loc[1]-1))
			if attr == nil {
				t.Errorf("%s: %s.%s has no name; terraform would generate one the policy can't anticipate", rel, kind, label)
				continue
			}
			name, ok := resourceName(attr[2], locals[filepath.Dir(f)])
			if !ok {
				t.Errorf("%s: %s.%s: can't read its name from %s; give it a string or a string local", rel, kind, label, attr[2])
				continue
			}
			if attr[1] == "name_prefix" {
				name += "x1"
			}
			arn := created.arn(name)
			if !allowed(t, doc, created.action, arn) {
				t.Errorf("%s: %s.%s: the provisioning policy doesn't allow %s on %s", rel, kind, label, created.action, arn)
			}
			checked++
		}
	}
	t.Logf("checked %d named resources in %d files", checked, len(files))
	require.Positive(t, checked)
}
