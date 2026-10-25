package clients

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNewExternalId(t *testing.T) {
	id, err := NewExternalId(bytes.NewReader(bytes.Repeat([]byte{0xab}, 16)))
	if err != nil {
		t.Fatal(err)
	}
	if id != "pn-"+strings.Repeat("ab", 16) {
		t.Fatalf("got %q", id)
	}
	if !ValidExternalId(id) {
		t.Fatalf("issued id %q should be valid", id)
	}
	if _, err := NewExternalId(bytes.NewReader([]byte{1, 2})); err == nil {
		t.Fatal("short randomness should fail")
	}
	a, _ := NewRandomExternalId()
	b, _ := NewRandomExternalId()
	if a == b || !ValidExternalId(a) {
		t.Fatalf("random ids should be valid and differ: %q %q", a, b)
	}
}

func TestValidExternalId(t *testing.T) {
	for _, bad := range []string{"", "pn-", "pn-" + strings.Repeat("a", 31), "pn-" + strings.Repeat("A", 32),
		"xx-" + strings.Repeat("a", 32), "pn-" + strings.Repeat("a", 33), "pn-" + strings.Repeat("g", 32)} {
		if ValidExternalId(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

type trustDoc struct {
	Statement []struct {
		Effect    string
		Principal map[string]string
		Action    string
		Condition map[string]map[string]string
	}
}

func TestTrustPolicy(t *testing.T) {
	legacy, err := TrustPolicy("111122223333", "")
	if err != nil {
		t.Fatal(err)
	}
	var d trustDoc
	if err := json.Unmarshal([]byte(legacy), &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Statement) != 1 || d.Statement[0].Principal["AWS"] != "111122223333" || d.Statement[0].Action != "sts:AssumeRole" {
		t.Fatalf("unexpected legacy policy: %s", legacy)
	}
	if d.Statement[0].Condition != nil {
		t.Fatalf("legacy policy should have no condition: %s", legacy)
	}

	withId, err := TrustPolicy("111122223333", "pn-"+strings.Repeat("0", 32))
	if err != nil {
		t.Fatal(err)
	}
	d = trustDoc{}
	if err := json.Unmarshal([]byte(withId), &d); err != nil {
		t.Fatal(err)
	}
	if got := d.Statement[0].Condition["StringEquals"]["sts:ExternalId"]; got != "pn-"+strings.Repeat("0", 32) {
		t.Fatalf("condition missing: %s", withId)
	}
}
