// Package noderecords removes what Pennsieve keeps for a compute node once
// the node is gone: after the provisioner tore it down (the EventBridge DELETE
// completion), or when its account's role no longer lets Pennsieve reach it.
package noderecords

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/pennsieve/account-service/internal/interactivedns"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
)

// Remove deletes the node's access records and the node, then the account's
// interactive DNS delegation when no interactive node remains. A failure to
// remove the access records is logged; the node is deleted regardless.
func Remove(ctx context.Context, cfg aws.Config, dynamoDBClient *dynamodb.Client, nodeStore store_dynamodb.NodeStore, nodeId string) error {
	// The node's account, read before it goes, decides the delegation cleanup.
	deletedNode, _ := nodeStore.GetById(ctx, nodeId)

	if nodeAccessTable := os.Getenv("NODE_ACCESS_TABLE"); nodeAccessTable != "" {
		nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(dynamoDBClient, nodeAccessTable)
		if err := nodeAccessStore.RemoveAllNodeAccess(ctx, nodeId); err != nil {
			log.Printf("Error removing access records for node %s: %v", nodeId, err)
		} else {
			log.Printf("Cleaned up all access records for node %s", nodeId)
		}
	} else {
		log.Printf("NODE_ACCESS_TABLE not configured, skipping access cleanup")
	}

	if err := nodeStore.Delete(ctx, nodeId); err != nil {
		log.Printf("Error deleting node %s from nodes table: %v", nodeId, err)
		return fmt.Errorf("failed to delete node: %w", err)
	}
	log.Printf("Deleted node %s and its access records from the database", nodeId)

	var accountStore store_dynamodb.DynamoDBStore
	if accountsTable := os.Getenv("ACCOUNTS_TABLE"); accountsTable != "" {
		accountStore = store_dynamodb.NewAccountDatabaseStore(dynamoDBClient, accountsTable)
	}
	RemoveInteractiveDelegationIfUnused(ctx, cfg, accountStore, nodeStore, deletedNode)
	return nil
}

// RemoveInteractiveDelegationIfUnused deletes the parent-zone NS delegation
// for the account's interactive subdomain when no interactive nodes remain for
// the AWS account (i.e. the per-account zone is gone or about to be). The
// delegation is per AWS account, which several account records can share, so
// every record's nodes count. Safe: if a node still needs it, or the
// configuration to find it is missing, nothing happens. Best-effort.
func RemoveInteractiveDelegationIfUnused(ctx context.Context, cfg aws.Config, accountStore store_dynamodb.DynamoDBStore, nodeStore store_dynamodb.NodeStore, deletedNode models.DynamoDBNode) {
	parentZoneID := os.Getenv("INTERACTIVE_PARENT_ZONE_ID")
	parentDomain := os.Getenv("INTERACTIVE_PARENT_DOMAIN")
	if parentZoneID == "" || parentDomain == "" || accountStore == nil || deletedNode.AccountId == "" {
		return
	}
	remaining, err := store_dynamodb.NodesInAWSAccount(ctx, accountStore, nodeStore, deletedNode.AccountId)
	if err != nil {
		log.Printf("Warning: could not list AWS account %s nodes for delegation cleanup: %v", deletedNode.AccountId, err)
		return
	}
	for _, n := range remaining {
		if n.MaxInteractiveSessions > 0 {
			return // another interactive node still needs the zone
		}
	}
	zoneName := fmt.Sprintf("%s.%s", deletedNode.AccountId, parentDomain)
	r53 := route53.NewFromConfig(cfg)
	if err := interactivedns.RemoveZoneDelegation(ctx, r53, parentZoneID, zoneName); err != nil {
		log.Printf("Warning: failed to remove NS delegation for %s: %v", zoneName, err)
		return
	}
	log.Printf("Removed interactive NS delegation for %s (no interactive nodes remain)", zoneName)
}
