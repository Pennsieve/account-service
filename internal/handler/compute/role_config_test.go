package compute

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestBuildRoleConfig_LegacyHasNoExternalId(t *testing.T) {
	called := false
	resp, err := buildRoleConfig("Pennsieve-Compute-dev-abcd1234", "111122223333", false, func() (string, error) {
		called = true
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if called || resp.ExternalId != "" {
		t.Fatalf("a legacy request must not get an external id: %+v", resp)
	}
	if strings.Contains(string(resp.TrustPolicyDocument), "Condition") {
		t.Fatalf("legacy trust policy should be unconditional: %s", resp.TrustPolicyDocument)
	}
	body, _ := json.Marshal(resp)
	if strings.Contains(string(body), "externalId") {
		t.Fatalf("legacy response should not carry externalId: %s", body)
	}
}

func TestBuildRoleConfig_WithExternalId(t *testing.T) {
	id := "pn-" + strings.Repeat("1", 32)
	resp, err := buildRoleConfig("Pennsieve-Compute-dev-abcd1234", "111122223333", true, func() (string, error) { return id, nil })
	if err != nil {
		t.Fatal(err)
	}
	if resp.ExternalId != id {
		t.Fatalf("want %s, got %s", id, resp.ExternalId)
	}
	var trust struct {
		Statement []struct {
			Principal map[string]string
			Condition map[string]map[string]string
		}
	}
	if err := json.Unmarshal(resp.TrustPolicyDocument, &trust); err != nil {
		t.Fatal(err)
	}
	if trust.Statement[0].Principal["AWS"] != "111122223333" || trust.Statement[0].Condition["StringEquals"]["sts:ExternalId"] != id {
		t.Fatalf("trust policy should require %s: %s", id, resp.TrustPolicyDocument)
	}
}

func TestBuildRoleConfig_GeneratorError(t *testing.T) {
	if _, err := buildRoleConfig("r", "1", true, func() (string, error) { return "", errors.New("no entropy") }); err == nil {
		t.Fatal("want error")
	}
}
