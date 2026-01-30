package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/service"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/test"
)

// Test script to debug missing nodes in workspace
func main() {
	// Setup test environment
	if os.Getenv("DYNAMODB_URL") == "" {
		os.Setenv("DYNAMODB_URL", "http://localhost:8000")
	}
	if os.Getenv("ENV") == "" {
		os.Setenv("ENV", "TEST")
	}

	ctx := context.Background()
	client := test.GetClient()

	// Setup test tables
	if err := test.SetupPackageTables(); err != nil {
		log.Fatalf("Failed to setup tables: %v", err)
	}

	// Test data
	ownerId := "owner123"
	nodeUuid := uuid.New().String()
	organizationId := "org456"

	// Initialize stores
	nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(client, "test-access-table")
	nodesStore := store_dynamodb.NewNodeDatabaseStore(client, "test-nodes-table")
	permissionService := service.NewPermissionService(nodeAccessStore, nil)

	fmt.Println("=== Testing Workspace Node Visibility ===")
	fmt.Printf("Owner: %s, Node: %s, Org: %s\n\n", ownerId, nodeUuid, organizationId)

	// Step 1: Create a compute node WITH organization
	fmt.Println("Step 1: Creating compute node in organization...")
	node := models.DynamoDBNode{
		Uuid:           nodeUuid,
		Name:           "Test Workspace Node",
		Description:    "Test node in organization",
		UserId:         ownerId,
		AccountUuid:    "account-uuid",
		OrganizationId: organizationId, // IMPORTANT: Node has organization
		Status:         "Enabled",
		CreatedAt:      time.Now().Format("2006-01-02T15:04:05Z"),
	}

	err := nodesStore.Put(ctx, node)
	if err != nil {
		log.Fatalf("Failed to create node: %v", err)
	}
	fmt.Printf("✓ Node created in organization %s\n", organizationId)

	// Step 2: Create owner access entry (simulating what happens during node creation)
	fmt.Println("Step 2: Creating owner access entry...")
	ownerAccess := models.NodeAccess{
		EntityId:       models.FormatEntityId(models.EntityTypeUser, ownerId),
		NodeId:         models.FormatNodeId(nodeUuid),
		EntityType:     models.EntityTypeUser,
		EntityRawId:    ownerId,
		NodeUuid:       nodeUuid,
		AccessType:     models.AccessTypeOwner,
		OrganizationId: organizationId, // IMPORTANT: Access entry has organization
		GrantedBy:      ownerId,
	}

	err = nodeAccessStore.GrantAccess(ctx, ownerAccess)
	if err != nil {
		log.Fatalf("Failed to create owner access: %v", err)
	}
	fmt.Printf("✓ Owner access created with OrganizationId: %s\n", organizationId)

	// Step 3: Test GetAccessibleNodes method
	fmt.Println("Step 3: Testing GetAccessibleNodes...")
	accessibleNodes, err := permissionService.GetAccessibleNodes(ctx, ownerId, organizationId)
	if err != nil {
		log.Fatalf("Failed to get accessible nodes: %v", err)
	}
	fmt.Printf("✓ GetAccessibleNodes returned %d nodes: %v\n", len(accessibleNodes), accessibleNodes)

	// Step 4: Check if our node is in the accessible nodes
	found := false
	for _, accessibleUuid := range accessibleNodes {
		if accessibleUuid == nodeUuid {
			found = true
			break
		}
	}
	fmt.Printf("✓ Our node found in accessible nodes: %t\n", found)

	// Step 5: Simulate the GET handler filtering logic
	fmt.Println("Step 4: Testing GET handler filtering logic...")
	filteredNodes := []models.DynamoDBNode{}
	
	for _, nodeUuid := range accessibleNodes {
		fetchedNode, err := nodesStore.GetById(ctx, nodeUuid)
		if err != nil {
			fmt.Printf("⚠ Error fetching node %s: %v\n", nodeUuid, err)
			continue
		}
		
		fmt.Printf("  Node %s: OrganizationId='%s' (expected='%s')\n", 
			fetchedNode.Uuid, fetchedNode.OrganizationId, organizationId)
		
		if fetchedNode.Uuid != "" && fetchedNode.OrganizationId == organizationId {
			filteredNodes = append(filteredNodes, fetchedNode)
			fmt.Printf("  ✓ Node %s included in results\n", fetchedNode.Uuid)
		} else {
			fmt.Printf("  ✗ Node %s filtered out (Uuid='%s', OrgId='%s')\n", 
				fetchedNode.Uuid, fetchedNode.Uuid, fetchedNode.OrganizationId)
		}
	}

	fmt.Printf("✓ Final filtered nodes: %d\n", len(filteredNodes))

	// Step 6: Debug access entries
	fmt.Println("Step 5: Debug access entries...")
	allAccess, err := nodeAccessStore.GetNodeAccess(ctx, nodeUuid)
	if err != nil {
		log.Fatalf("Failed to get access entries: %v", err)
	}
	
	fmt.Printf("✓ Node has %d access entries:\n", len(allAccess))
	for _, access := range allAccess {
		fmt.Printf("  - EntityType: %s, EntityRawId: %s, AccessType: %s, OrgId: '%s'\n",
			access.EntityType, access.EntityRawId, access.AccessType, access.OrganizationId)
	}

	// Step 7: Summary
	fmt.Println("\n=== SUMMARY ===")
	if len(filteredNodes) == 1 && filteredNodes[0].Uuid == nodeUuid {
		fmt.Println("🎉 SUCCESS: Node owner can see their node in workspace!")
	} else {
		fmt.Println("❌ PROBLEM: Node owner cannot see their node in workspace!")
		fmt.Println("This explains why you don't see your nodes when querying the workspace.")
		
		// Diagnostic info
		fmt.Println("\nDiagnostic Information:")
		fmt.Printf("- Node OrganizationId: '%s'\n", node.OrganizationId)
		fmt.Printf("- Expected OrganizationId: '%s'\n", organizationId)
		fmt.Printf("- Access entries: %d\n", len(allAccess))
		fmt.Printf("- Accessible nodes returned: %d\n", len(accessibleNodes))
		fmt.Printf("- Nodes after filtering: %d\n", len(filteredNodes))
	}
}