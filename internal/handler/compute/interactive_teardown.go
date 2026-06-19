package compute

import (
	"context"

	"github.com/pennsieve/account-service/internal/store_dynamodb"
)

// accountHasInteractiveExcept reports whether any node in the account — other
// than excludeUuid — currently has interactive sessions enabled.
//
// Used to decide whether removing/disabling interactive on one node leaves the
// account's shared interactive infrastructure unused. When it returns false, the
// caller sets DESTROY_INTERACTIVE=true on the node's own provisioner run so the
// provisioner tears down the (now unused) shared interactive infra. Pass the
// empty string for excludeUuid to consider every node.
func accountHasInteractiveExcept(ctx context.Context, nodeStore store_dynamodb.NodeStore, accountUuid, excludeUuid string) (bool, error) {
	nodes, err := nodeStore.GetByAccount(ctx, accountUuid)
	if err != nil {
		return false, err
	}
	for _, n := range nodes {
		if n.Uuid == excludeUuid {
			continue
		}
		if n.MaxInteractiveSessions > 0 {
			return true, nil
		}
	}
	return false, nil
}
