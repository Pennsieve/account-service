# Account Service Migration Guide

## Overview
This guide describes the migration from workspace-coupled accounts to user-owned accounts with separate workspace enablement.

## Architecture Changes

### Before (Old Model)
- Accounts stored with `organizationId` directly in the accounts table
- One account per user per workspace (duplicates if shared across workspaces)
- No visibility control (all accounts visible to workspace)

### After (New Model)
- Accounts owned by users (no `organizationId` in accounts table)
- Separate `account_workspace_enablement` table for workspace access
- Single account can be enabled for multiple workspaces
- Public/private visibility control per workspace

## Migration Strategy

### Phase 1: Deploy New Code (Backward Compatible)
1. Deploy the updated service with both old and new endpoints
2. The service will handle both data models:
   - Old records with `organizationId` field
   - New records without `organizationId` + enablement table

### Phase 2: Run Migration Script
```bash
# Set environment variables
export ACCOUNTS_TABLE=your-accounts-table
export ACCOUNT_WORKSPACE_TABLE=your-workspace-table

# Run in dry-run mode first
export DRY_RUN=true
go run cmd/migration/migrate-accounts/

# Run actual migration
export DRY_RUN=false
go run cmd/migration/migrate-accounts/
```

The migration script will:
1. Scan all accounts with `organizationId`
2. Create workspace enablement records
3. Remove `organizationId` from account records
4. Handle duplicates and already-migrated records

### Phase 3: Monitor and Validate
- Check CloudWatch logs for any errors
- Verify accounts are accessible through new endpoints
- Confirm workspace enablements are created correctly

### Phase 4: Deprecate Legacy Endpoints
Once all clients are updated:
1. Remove legacy handler from router
2. Clean up backward compatibility code
3. Remove old Get() method from store

## Rollback Plan

If issues occur during migration:

1. **Before Migration Script**: Just redeploy old code
2. **During Migration**: Script is idempotent and can be re-run
3. **After Migration**: 
   - Restore from DynamoDB backup
   - Or run reverse migration to add `organizationId` back

## API Changes

### Deprecated Endpoints
- `GET /accounts` (with org-based filtering) → Use user-based filtering

### New Endpoints
- `POST /accounts/{uuid}/workspaces` - Enable account for workspace
- `DELETE /accounts/{uuid}/workspaces/{workspaceId}` - Disable account
- `GET /accounts?workspace={id}` - Get user accounts filtered by workspace
- `GET /accounts?includeWorkspaces=true` - Get accounts with workspace details

## Client Updates Required

Clients need to:
1. Register accounts without workspace (POST /accounts)
2. Separately enable for workspaces (POST /accounts/{uuid}/workspaces)
3. Handle new response format with workspace enablements

## Timeline

- **Week 1**: Deploy backward-compatible service
- **Week 2**: Run migration in dev/staging
- **Week 3**: Run migration in production
- **Week 4**: Update clients to use new endpoints
- **Week 5**: Remove legacy code

## Monitoring

Watch for:
- Increased DynamoDB errors
- Migration script completion metrics
- API latency changes
- Client error rates

## Migration Tools

The project includes several migration tools in the `cmd/migration/` directory:

### Account Migration
```bash
# Migrate accounts from old to new model
go run cmd/migration/migrate-accounts/
```

### Workspace Enablements Creation
```bash
# Create workspace enablements from export data
go run cmd/migration/create-workspace-enablements/ <export_directory>
```

### Table Import
```bash
# Import account data from export files
go run cmd/migration/import-tables/ <export_directory>
```

## Project Structure

The service has been reorganized to follow Go standard project layout:

```
├── cmd/                          # Executables
│   ├── api/                     # Main Lambda service
│   └── migration/               # Migration tools
│       ├── migrate-accounts/    # Account migration tool
│       ├── create-workspace-enablements/  # Workspace enablement tool
│       └── import-tables/       # Table import tool
├── internal/                    # Private application code
│   ├── handler/                 # Lambda handlers
│   ├── models/                  # Data models
│   ├── store_dynamodb/          # DynamoDB operations
│   ├── utils/                   # Utilities
│   ├── logging/                 # Logging setup
│   └── mappers/                 # Data mappers
├── go.mod                       # Go module (at root)
└── Makefile                     # Build commands
```

## Support

For issues during migration:
1. Check CloudWatch logs
2. Verify table permissions
3. Confirm GSI creation on accounts table
4. Check workspace enablement table exists