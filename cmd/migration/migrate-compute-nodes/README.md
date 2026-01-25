# Compute Nodes Migration Scripts

These scripts help migrate compute nodes data from the old compute-node-service DynamoDB table to the new account-service compute_resource_nodes_table.

## Files

- `export/main.go` - Exports compute nodes from the old table to a JSON file
- `import/main.go` - Imports compute nodes from JSON file to the new table
- `export/types.go` and `import/types.go` - Shared data structures used by both scripts

## Prerequisites

1. AWS credentials configured (via AWS CLI, IAM role, or environment variables)
2. Access to both source and target DynamoDB tables
3. Go 1.19+ installed

## Usage

### Step 1: Export from Old Table

```bash
# Build the export tool
go build -o export ./export

# Set environment variables and run export
export SOURCE_COMPUTE_NODES_TABLE="dev-compute-nodes-table-use1"  # Old table name
export OUTPUT_FILE="compute_nodes_backup.json"                     # Optional, defaults to compute_nodes_export.json

# Run the export
./export
```

### Step 2: Import to New Table

```bash
# Build the import tool
go build -o import ./import

# Set environment variables
export TARGET_COMPUTE_NODES_TABLE="dev-compute-resource-nodes-table-use1"  # New table name
export INPUT_FILE="compute_nodes_backup.json"                              # Optional, defaults to compute_nodes_export.json

# Optional: Run a dry run first to see what would be imported
export DRY_RUN="true"
./import

# Run the actual import
unset DRY_RUN
export SKIP_EXISTING="true"  # Optional: skip nodes that already exist
./import
```

## Environment Variables

### Export Script (`export/main.go`)

- `SOURCE_COMPUTE_NODES_TABLE` (required) - Name of the source DynamoDB table
- `OUTPUT_FILE` (optional) - Output JSON file path (default: `compute_nodes_export.json`)

### Import Script (`import/main.go`)

- `TARGET_COMPUTE_NODES_TABLE` (required) - Name of the target DynamoDB table
- `INPUT_FILE` (optional) - Input JSON file path (default: `compute_nodes_export.json`)
- `DRY_RUN` (optional) - Set to "true" to perform a dry run without making changes
- `SKIP_EXISTING` (optional) - Set to "true" to skip nodes that already exist in target table

## Data Structure

Both scripts work with the `DynamoDBNode` structure which includes:

```go
type DynamoDBNode struct {
    Uuid                  string `dynamodbav:"uuid" json:"uuid"`
    Name                  string `dynamodbav:"name" json:"name"`
    Description           string `dynamodbav:"description" json:"description"`
    ComputeNodeGatewayUrl string `dynamodbav:"computeNodeGatewayUrl" json:"computeNodeGatewayUrl"`
    EfsId                 string `dynamodbav:"efsId" json:"efsId"`
    QueueUrl              string `dynamodbav:"queueUrl" json:"queueUrl"`
    Env                   string `dynamodbav:"environment" json:"environment"`
    AccountUuid           string `dynamodbav:"accountUuid" json:"accountUuid"`
    AccountId             string `dynamodbav:"accountId" json:"accountId"`
    AccountType           string `dynamodbav:"accountType" json:"accountType"`
    CreatedAt             string `dynamodbav:"createdAt" json:"createdAt"`
    OrganizationId        string `dynamodbav:"organizationId" json:"organizationId"`
    UserId                string `dynamodbav:"userId" json:"userId"`
    Identifier            string `dynamodbav:"identifier" json:"identifier"`
    WorkflowManagerTag    string `dynamodbav:"workflowManagerTag" json:"workflowManagerTag"`
    Status                string `dynamodbav:"status" json:"status"`
    TimeToExist           int64  `dynamodbav:"TimeToExist,omitempty" json:"timeToExist,omitempty"`
}
```

### Status Field Handling

The import script automatically ensures all compute nodes have a `Status` field:
- If a node already has a `status` value, it is preserved
- If a node has an empty or missing `status` field, it is automatically set to `"Enabled"`
- This ensures all migrated nodes have a valid status in the new table

## Safety Features

- **Dry Run**: Test the import process without making actual changes
- **Skip Existing**: Avoid overwriting existing nodes in the target table
- **Error Handling**: Detailed logging and error reporting
- **Batch Processing**: Progress updates every 50/100 items for large datasets

## Example Migration Flow

```bash
# 1. Export from production old table
export SOURCE_COMPUTE_NODES_TABLE="prod-compute-nodes-table-use1"
export OUTPUT_FILE="prod_compute_nodes_backup_$(date +%Y%m%d).json"
go run ./export

# 2. Test import with dry run
export TARGET_COMPUTE_NODES_TABLE="prod-compute-resource-nodes-table-use1"
export INPUT_FILE="prod_compute_nodes_backup_$(date +%Y%m%d).json"
export DRY_RUN="true"
go run ./import

# 3. Perform actual import
unset DRY_RUN
export SKIP_EXISTING="true"
go run ./import
```

## Notes

- The scripts preserve all original data including TTL settings (`TimeToExist`)
- Both tables should have the same schema for successful migration
- Consider backing up the target table before running the import
- Monitor AWS CloudWatch for DynamoDB throttling during large migrations