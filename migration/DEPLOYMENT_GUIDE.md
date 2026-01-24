# Table Migration Deployment Guide

## Overview
This guide walks through migrating the accounts table to the new naming structure.

## Table Changes
- **Accounts table**: `accounts-table` → `compute-resource-accounts-table`
- **NEW Workspace table**: `compute-resource-account-workspace-table` (created fresh, no migration needed)
  - Uses `workspaceId` instead of `organizationId` as range key

## Deployment Steps

### 1. Export Current Data (BEFORE deploying Terraform changes)

```bash
cd migration

# For dev environment
./export_tables.sh dev

# For staging
./export_tables.sh staging

# For production
./export_tables.sh prod
```

This creates timestamped exports in `exports/` directory with only the accounts table data.

### 2. Deploy Terraform Changes

```bash
cd terraform

# This will:
# - DESTROY old accounts-table
# - CREATE new compute-resource-accounts-table  
# - CREATE new compute-resource-account-workspace-table (empty)
terraform apply
```

### 3. Import Accounts Data

```bash
cd migration

# Use the export directory from step 1
# For dev
ENV=dev go run import_tables.go exports/dev_[TIMESTAMP]

# For staging  
ENV=staging go run import_tables.go exports/staging_[TIMESTAMP]

# For production
ENV=prod go run import_tables.go exports/prod_[TIMESTAMP]
```

**Note:** The import script will:
- Remove `organizationId` field from accounts (not needed in new model)
- Set default userId `N:user:9e8ecf93-62cf-41bf-9f32-99542acda06c` for accounts without a user

### 4. Create Workspace Enablements (Optional)

If your old accounts had `organizationId` fields, run this to create workspace enablements:

```bash
cd migration

# This creates workspace enablement records based on old organizationId values
ENV=dev go run create_workspace_enablements.go exports/dev_[TIMESTAMP]
```

This will enable the accounts for their original workspaces with `isPublic: true`.

### 4. Deploy Lambda Code

Deploy the updated Lambda function with the new code that handles the table/column changes.

```bash
make package
make publish
```

### 5. Verify

Test the endpoints to ensure data is accessible:

```bash
# Get accounts
curl -H "Authorization: Bearer $TOKEN" \
  https://api.domain.com/compute/resources/accounts

# Get workspace enablements (if you have the account UUID)
curl -H "Authorization: Bearer $TOKEN" \
  https://api.domain.com/compute/resources/accounts?workspace=YOUR_WORKSPACE_ID
```

## Rollback Plan

If something goes wrong:

1. Keep the export files safe - they are your backup
2. To rollback Terraform: 
   - Revert the terraform changes
   - Run `terraform apply` to recreate old tables
   - Re-import data using a modified import script (change table names back)

## Important Notes

- **ALWAYS export data before applying Terraform changes**
- The import script automatically handles the `organizationId` → `workspaceId` transformation
- Keep export files for at least 30 days as backup
- Test in dev/staging before production deployment