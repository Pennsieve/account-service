package compute

import (
	"context"

	"github.com/pennsieve/account-service/internal/store_dynamodb"
)

// accountHasInteractiveExcept reports whether any node in the AWS account —
// other than excludeUuid — currently has interactive sessions enabled. The
// shared interactive infra is per AWS account, which several account records
// can share, so the nodes of every record count.
//
// Used to decide whether removing/disabling interactive on one node leaves the
// account's shared interactive infrastructure unused. When it returns false, the
// caller sets DESTROY_INTERACTIVE=true on the node's own provisioner run so the
// provisioner tears down the (now unused) shared interactive infra. Pass the
// empty string for excludeUuid to consider every node.
func accountHasInteractiveExcept(ctx context.Context, accountStore store_dynamodb.DynamoDBStore, nodeStore store_dynamodb.NodeStore, awsAccountId, excludeUuid string) (bool, error) {
	nodes, err := store_dynamodb.NodesInAWSAccount(ctx, accountStore, nodeStore, awsAccountId)
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
