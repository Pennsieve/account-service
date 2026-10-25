package clients

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"time"
)

// ExternalIdLifetime is how long an issued external ID can register its role:
// enough to create the role and register it, not a standing credential.
const ExternalIdLifetime = 24 * time.Hour

// An account's sts:ExternalId: issued by Pennsieve when the agent asks for a
// role configuration, written into the role's trust policy, and sent with every
// AssumeRole into it. A role that requires it can't be used by Pennsieve on
// behalf of anyone who registers someone else's account and role, the
// confused-deputy problem. Accounts registered before it existed have none.
var externalIdPattern = regexp.MustCompile(`^pn-[0-9a-f]{32}$`)

// ValidExternalId reports whether id has the shape Pennsieve issues.
func ValidExternalId(id string) bool {
	return externalIdPattern.MatchString(id)
}

// NewExternalId returns a fresh external ID ("pn-" and 32 hex characters),
// reading randomness from r (crypto/rand.Reader outside tests).
func NewExternalId(r io.Reader) (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", fmt.Errorf("generating external id: %w", err)
	}
	return "pn-" + hex.EncodeToString(b), nil
}

// NewRandomExternalId is NewExternalId from crypto/rand.
func NewRandomExternalId() (string, error) { return NewExternalId(rand.Reader) }

// TrustPolicy is the trust policy of a user's compute role: Pennsieve's
// account may assume it, and, with an externalId, only when it sends that ID.
func TrustPolicy(pennsieveAccountId, externalId string) (string, error) {
	statement := map[string]any{
		"Effect":    "Allow",
		"Principal": map[string]string{"AWS": pennsieveAccountId},
		"Action":    "sts:AssumeRole",
	}
	if externalId != "" {
		statement["Condition"] = map[string]any{
			"StringEquals": map[string]string{"sts:ExternalId": externalId},
		}
	}
	doc, err := json.MarshalIndent(map[string]any{
		"Version":   "2012-10-17",
		"Statement": []any{statement},
	}, "", "\t")
	if err != nil {
		return "", err
	}
	return string(doc), nil
}
