package main

import (
    "context"
    "flag"
    "fmt"
    "log"
    "os"
    "time"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
    "github.com/pennsieve/account-service/internal/models"
    "github.com/pennsieve/account-service/internal/store_dynamodb"
)

// MigrationConfig holds configuration for the migration
type MigrationConfig struct {
    NodesTableName  string
    AccessTableName string
    Environment     string
    DryRun          bool
    BatchSize       int
}

func main() {
    log.Println("=== Enable Organization Access Migration ===")
    log.Println("This script migrates imported compute nodes to be available throughout their organization")

    // Parse command line flags
    var cfg MigrationConfig
    flag.StringVar(&cfg.NodesTableName, "nodes-table", "", "DynamoDB compute nodes table name (required)")
    flag.StringVar(&cfg.AccessTableName, "access-table", "", "DynamoDB node access table name (required)")
    flag.StringVar(&cfg.Environment, "env", "dev", "Environment (dev/staging/prod)")
    flag.BoolVar(&cfg.DryRun, "dry-run", false, "Perform dry run without making changes")
    flag.IntVar(&cfg.BatchSize, "batch-size", 10, "Number of nodes to process in each batch")

    flag.Parse()

    // Validate required flags
    if cfg.NodesTableName == "" || cfg.AccessTableName == "" {
        fmt.Println("Usage: go run main.go -nodes-table <name> -access-table <name> [options]")
        fmt.Println()
        fmt.Println("This migration script enables organization-wide access for imported compute nodes.")
        fmt.Println("It adds workspace-level access permissions so all organization members can access the nodes.")
        fmt.Println()
        fmt.Println("Required flags:")
        fmt.Println("  -nodes-table    DynamoDB compute nodes table name")
        fmt.Println("  -access-table   DynamoDB node access table name")
        fmt.Println()
        fmt.Println("Optional flags:")
        fmt.Println("  -env            Environment (default: dev)")
        fmt.Println("  -dry-run        Perform dry run without making changes")
        fmt.Println("  -batch-size     Batch size for processing (default: 10)")
        fmt.Println("  -org            Only migrate nodes from specific organization")
        fmt.Println()
        fmt.Println("Examples:")
        fmt.Println("  # Dry run for all organizations")
        fmt.Println("  go run main.go -nodes-table prod-nodes -access-table prod-access -dry-run")
        fmt.Println()
        fmt.Println("  # Migrate specific organization")
        fmt.Println("  go run main.go -nodes-table prod-nodes -access-table prod-access -org org-123")
        os.Exit(1)
    }

    log.Printf("Configuration:")
    log.Printf("  Nodes Table: %s", cfg.NodesTableName)
    log.Printf("  Access Table: %s", cfg.AccessTableName)
    log.Printf("  Environment: %s", cfg.Environment)
    log.Printf("  Dry Run: %v", cfg.DryRun)
    log.Printf("  Batch Size: %d", cfg.BatchSize)
    log.Println()

    if cfg.DryRun {
        log.Println("⚠️  DRY RUN MODE - No changes will be made")
    } else {
        log.Println("🚨 LIVE MODE - Changes will be applied!")
        fmt.Print("Continue? (y/N): ")
        var response string
        fmt.Scanln(&response)
        if response != "y" && response != "Y" {
            log.Println("Migration cancelled")
            os.Exit(0)
        }
    }

    // Initialize AWS config
    ctx := context.Background()
    awsCfg, err := config.LoadDefaultConfig(ctx)
    if err != nil {
        log.Fatalf("Failed to load AWS config: %v", err)
    }

    // Initialize DynamoDB client
    dynamoClient := dynamodb.NewFromConfig(awsCfg)

    // Initialize stores
    nodeStore := store_dynamodb.NewNodeDatabaseStore(dynamoClient, cfg.NodesTableName)
    accessStore := store_dynamodb.NewNodeAccessDatabaseStore(dynamoClient, cfg.AccessTableName)

    // Run migration
    if err := runMigration(ctx, &cfg, nodeStore, accessStore); err != nil {
        log.Fatalf("Migration failed: %v", err)
    }

    log.Println("✅ Migration completed successfully!")
}

func runMigration(ctx context.Context, cfg *MigrationConfig, nodeStore store_dynamodb.NodeStore, accessStore store_dynamodb.NodeAccessStore) error {
    log.Println("🔍 Scanning all compute nodes to find organizations and migration targets...")
    log.Println("📋 This will scan the entire nodes table to discover all organizations automatically")

    // Scan all nodes from the table to discover organizations
    allNodes, err := scanAllNodes(ctx, nodeStore)
    if err != nil {
        return fmt.Errorf("failed to scan all nodes: %w", err)
    }

    log.Printf("📊 Found %d total nodes in the table", len(allNodes))

    // Group nodes by organization and count them
    orgCounts := make(map[string]int)
    orgIndependentCount := 0

    for _, node := range allNodes {
        if node.OrganizationId == "" {
            orgIndependentCount++
        } else {
            orgCounts[node.OrganizationId]++
        }
    }

    log.Printf("🏢 Organizations discovered:")
    for orgId, count := range orgCounts {
        log.Printf("  • %s: %d nodes", orgId, count)
    }
    if orgIndependentCount > 0 {
        log.Printf("  • Organization-independent: %d nodes", orgIndependentCount)
    }
    log.Println()

    // Filter nodes that need migration (those without workspace access)
    nodesToMigrate := []models.DynamoDBNode{}
    migratedCount := 0
    skippedCount := 0

    for _, node := range allNodes {
        // Skip organization-independent nodes
        if node.OrganizationId == "" {
            skippedCount++
            log.Printf("  ⏭️  Skipping organization-independent node: %s (%s)", node.Uuid, node.Name)
            continue
        }

        // Check if node already has workspace access
        workspaceEntityId := models.FormatEntityId(models.EntityTypeWorkspace, node.OrganizationId)
        nodeId := models.FormatNodeId(node.Uuid)

        existingAccess, err := accessStore.GetNodeAccess(ctx, nodeId)
        if err != nil {
            log.Printf("  ⚠️  Could not check existing access for node %s: %v", node.Uuid, err)
            continue
        }

        // Check if workspace access already exists for this node
        hasWorkspaceAccess := false
        for _, access := range existingAccess {
            if access.EntityId == workspaceEntityId && access.AccessType == models.AccessTypeWorkspace {
                hasWorkspaceAccess = true
                break
            }
        }

        if hasWorkspaceAccess {
            skippedCount++
            log.Printf("  ✅ Node already has workspace access: %s (%s)", node.Uuid, node.Name)
            continue
        }

        nodesToMigrate = append(nodesToMigrate, node)
    }

    log.Println()
    log.Printf("📈 Migration Summary:")
    log.Printf("  Total nodes found: %d", len(allNodes))
    log.Printf("  Nodes to migrate: %d", len(nodesToMigrate))
    log.Printf("  Nodes skipped: %d", skippedCount)
    log.Println()

    if len(nodesToMigrate) == 0 {
        log.Println("🎉 No nodes need migration!")
        return nil
    }

    if cfg.DryRun {
        log.Println("🔍 DRY RUN - Would migrate the following nodes:")
        for _, node := range nodesToMigrate {
            log.Printf("  - %s (%s) in org %s - created by user %s",
                node.Uuid, node.Name, node.OrganizationId, node.UserId)
        }
        log.Println()
        log.Println("💡 Migration would:")
        log.Println("  • Add workspace-level access for each node")
        log.Println("  • Allow all organization members to access these nodes")
        log.Println("  • Preserve existing owner and shared access permissions")
        return nil
    }

    // Perform actual migration
    log.Printf("🚀 Starting migration of %d nodes...", len(nodesToMigrate))
    log.Println()

    for i, node := range nodesToMigrate {
        log.Printf("  [%d/%d] Migrating node %s (%s)...", i+1, len(nodesToMigrate), node.Uuid, node.Name)

        if err := migrateNode(ctx, accessStore, node); err != nil {
            log.Printf("    ❌ Failed to migrate node %s: %v", node.Uuid, err)
            continue
        }

        migratedCount++
        log.Printf("    ✅ Successfully migrated node %s", node.Uuid)

        // Add small delay to avoid overwhelming DynamoDB
        if (i+1)%cfg.BatchSize == 0 {
            log.Printf("  💤 Processed batch of %d nodes, sleeping for 1 second...", cfg.BatchSize)
            time.Sleep(1 * time.Second)
        }
    }

    log.Println()
    log.Printf("🎉 Migration completed! Successfully migrated %d out of %d nodes", migratedCount, len(nodesToMigrate))

    if migratedCount < len(nodesToMigrate) {
        log.Printf("⚠️  %d nodes failed to migrate - check logs above", len(nodesToMigrate)-migratedCount)
        return fmt.Errorf("migration partially failed: %d nodes could not be migrated", len(nodesToMigrate)-migratedCount)
    }

    log.Println()
    log.Println("✨ All nodes now have organization-wide access!")
    log.Println("🔍 Organization members can now access these compute nodes through the workspace")

    return nil
}

func migrateNode(ctx context.Context, accessStore store_dynamodb.NodeAccessStore, node models.DynamoDBNode) error {
    // Create workspace access entry
    workspaceAccess := models.NodeAccess{
        EntityId:       models.FormatEntityId(models.EntityTypeWorkspace, node.OrganizationId),
        NodeId:         models.FormatNodeId(node.Uuid),
        EntityType:     models.EntityTypeWorkspace,
        EntityRawId:    node.OrganizationId,
        NodeUuid:       node.Uuid,
        AccessType:     models.AccessTypeWorkspace,
        OrganizationId: node.OrganizationId,
        GrantedAt:      time.Now(),
        GrantedBy:      "migration-script", // Special identifier for migration
    }

    // Grant workspace access
    if err := accessStore.GrantAccess(ctx, workspaceAccess); err != nil {
        return fmt.Errorf("failed to grant workspace access: %w", err)
    }

    return nil
}

// scanAllNodes scans the entire nodes table to get all nodes
// This is used instead of the Get method which filters by organizationId
func scanAllNodes(ctx context.Context, nodeStore store_dynamodb.NodeStore) ([]models.DynamoDBNode, error) {
    // We need to access the underlying DynamoDB client directly
    // Cast the interface to the concrete type to access the DB client
    nodeStoreImpl, ok := nodeStore.(*store_dynamodb.NodeDatabaseStore)
    if !ok {
        return nil, fmt.Errorf("could not cast node store to concrete implementation")
    }

    var allNodes []models.DynamoDBNode
    var lastEvaluatedKey map[string]types.AttributeValue

    for {
        // Build scan input
        scanInput := &dynamodb.ScanInput{
            TableName: aws.String(nodeStoreImpl.TableName),
        }

        // Add pagination if we have a key from the previous scan
        if lastEvaluatedKey != nil {
            scanInput.ExclusiveStartKey = lastEvaluatedKey
        }

        // Perform scan
        result, err := nodeStoreImpl.DB.Scan(ctx, scanInput)
        if err != nil {
            return nil, fmt.Errorf("error scanning nodes table: %w", err)
        }

        // Unmarshal the results
        var batchNodes []models.DynamoDBNode
        err = attributevalue.UnmarshalListOfMaps(result.Items, &batchNodes)
        if err != nil {
            return nil, fmt.Errorf("error unmarshaling nodes: %w", err)
        }

        allNodes = append(allNodes, batchNodes...)

        // Check if we need to continue scanning
        if result.LastEvaluatedKey == nil {
            break
        }
        lastEvaluatedKey = result.LastEvaluatedKey
    }

    return allNodes, nil
}
