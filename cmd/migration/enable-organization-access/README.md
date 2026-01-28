# Enable Organization Access Migration

This migration script enables organization-wide access for imported compute nodes by adding workspace-level access permissions.

## Purpose

When compute nodes are imported from external systems, they may not have proper workspace-level access configured. This script ensures that all members of an organization can access the imported compute nodes within their workspace.

## What it does

- Scans compute nodes in a specified organization
- Identifies nodes that don't have workspace-level access
- Adds workspace access permissions to make nodes available to all organization members
- Preserves existing owner and individual sharing permissions

## Usage

### Dry Run (Recommended First)

```bash
cd cmd/migration/enable-organization-access

# Check what would be migrated without making changes
go run main.go -nodes-table <nodes-table-name> -access-table <access-table-name> -org <org-id> -dry-run
```

### Production Migration

```bash
# Migrate nodes for a specific organization
go run main.go -nodes-table <nodes-table-name> -access-table <access-table-name> -org <org-id>
```

## Command Line Options

| Flag | Description | Required | Default |
|------|-------------|----------|---------|
| `-nodes-table` | DynamoDB compute nodes table name | Yes | - |
| `-access-table` | DynamoDB node access table name | Yes | - |
| `-org` | Organization ID to migrate | Yes | - |
| `-dry-run` | Perform dry run without making changes | No | false |
| `-batch-size` | Number of nodes to process in each batch | No | 10 |
| `-env` | Environment identifier | No | dev |

## Examples

### Development Environment

```bash
# Dry run for development
go run main.go \
  -nodes-table dev-compute-resource-nodes-table-us-east-1 \
  -access-table dev-compute-node-access-table-us-east-1 \
  -org N:organization:12345 \
  -dry-run

# Actual migration
go run main.go \
  -nodes-table dev-compute-resource-nodes-table-us-east-1 \
  -access-table dev-compute-node-access-table-us-east-1 \
  -org N:organization:12345
```

### Production Environment

```bash
# Always dry run first in production!
go run main.go \
  -nodes-table prod-compute-resource-nodes-table-us-east-1 \
  -access-table prod-compute-node-access-table-us-east-1 \
  -org N:organization:12345 \
  -env prod \
  -dry-run

# Run actual migration after reviewing dry run results
go run main.go \
  -nodes-table prod-compute-resource-nodes-table-us-east-1 \
  -access-table prod-compute-node-access-table-us-east-1 \
  -org N:organization:12345 \
  -env prod
```

## Safety Features

1. **Dry Run Mode**: Always test with `-dry-run` first
2. **Confirmation Prompt**: Requires user confirmation before making changes
3. **Batch Processing**: Processes nodes in batches with delays to avoid overwhelming DynamoDB
4. **Skip Existing**: Automatically skips nodes that already have workspace access
5. **Error Handling**: Continues processing other nodes if individual migrations fail

## Expected Output

### Dry Run Output
```
=== Enable Organization Access Migration ===
Configuration:
  Nodes Table: dev-compute-resource-nodes-table-us-east-1
  Access Table: dev-compute-node-access-table-us-east-1
  Organization: N:organization:12345
  Dry Run: true

⚠️  DRY RUN MODE - No changes will be made
🔍 Scanning for compute nodes to migrate...
📊 Found 15 nodes in organization N:organization:12345

📈 Migration Summary:
  Total nodes found: 15
  Nodes to migrate: 8
  Nodes skipped: 7

🔍 DRY RUN - Would migrate the following nodes:
  - node-uuid-1 (ML Training Node) in org N:organization:12345 - created by user N:user:789
  - node-uuid-2 (Data Processing Node) in org N:organization:12345 - created by user N:user:456
  ...

💡 Migration would:
  • Add workspace-level access for each node
  • Allow all organization members to access these nodes
  • Preserve existing owner and shared access permissions

✅ Migration completed successfully!
```

### Live Migration Output
```
🚀 Starting migration of 8 nodes...

  [1/8] Migrating node node-uuid-1 (ML Training Node)...
    ✅ Successfully migrated node node-uuid-1
  [2/8] Migrating node node-uuid-2 (Data Processing Node)...
    ✅ Successfully migrated node node-uuid-2
  ...

🎉 Migration completed! Successfully migrated 8 out of 8 nodes

✨ All nodes now have organization-wide access!
🔍 Organization members can now access these compute nodes through the workspace
```

## Troubleshooting

### Common Issues

1. **"No specific organization provided"**
   - Use the `-org` flag with a valid organization ID

2. **AWS Authentication Errors**
   - Ensure AWS credentials are configured (`aws configure` or environment variables)
   - Verify permissions to read/write the specified DynamoDB tables

3. **"Failed to get nodes for organization"**
   - Verify the organization ID format (should be like `N:organization:12345`)
   - Check that the nodes table name is correct

4. **Individual node migration failures**
   - Check DynamoDB permissions for the access table
   - Verify node UUIDs are valid

### Verification

After running the migration, you can verify success by:

1. **Using the GET /compute-nodes endpoint**:
   ```bash
   curl -H "Authorization: Bearer <token>" \
     "https://api.pennsieve.io/compute/resources/compute-nodes?organization_id=N:organization:12345"
   ```

2. **Checking DynamoDB directly** for workspace access entries:
   - Look for entries with `entityId` = `workspace#N:organization:12345`
   - Verify `accessType` = `workspace`

## Rollback

If you need to rollback the migration:

1. **Manual Rollback**: Delete the workspace access entries from the node access table
2. **Script Rollback**: A rollback script could be created to remove workspace access entries added by this migration (identified by `grantedBy` = `migration-script`)

## Migration Log

Keep track of migrations performed:

| Date | Environment | Organization | Nodes Migrated | Operator |
|------|-------------|-------------|----------------|----------|
| 2026-01-27 | dev | N:organization:12345 | 8 | username |
| | | | | |