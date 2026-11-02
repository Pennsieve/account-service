// Package platformnode names the platform compute node: the one node, in
// the Pennsieve Admin workspace, that every authenticated user may use.
//
// Its uuid is per-environment configuration (PLATFORM_COMPUTE_NODE_UUID,
// from terraform), not an attribute on the node record: there is no index
// to find a flagged record by, a stray write to the wrong record would open
// a customer's node to the whole platform, and one table value cannot differ
// between dev and prod. Opening a node to everyone is a reviewed diff.
//
// The designation changes one thing: who may use the node. The node is
// otherwise an ordinary node in its workspace, with its tenant, its settings
// and its spend caps; a run on it belongs to the tenant that declared it.
// Empty means there is no platform node.
package platformnode

import (
	"os"
	"strings"
)

const EnvVar = "PLATFORM_COMPUTE_NODE_UUID"

// Uuid is the platform node's uuid, or "" when there is none.
func Uuid() string { return strings.TrimSpace(os.Getenv(EnvVar)) }

// Is reports whether nodeUuid is the platform node.
func Is(nodeUuid string) bool {
	u := Uuid()
	return u != "" && nodeUuid == u
}
