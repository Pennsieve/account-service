#!/bin/bash

# Enable Organization Access Migration Runner
# This script provides an easy way to run the migration with common configurations

set -e

# Default values
ENVIRONMENT="dev"
DRY_RUN="true"
BATCH_SIZE="10"

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

usage() {
    echo "Usage: $0 -e <env> [options]"
    echo ""
    echo "Enable organization-wide access for imported compute nodes"
    echo "This script automatically discovers all organizations and migrates their nodes"
    echo ""
    echo "Required:"
    echo "  -e, --env       Environment (dev, staging, prod)"
    echo ""
    echo "Options:"
    echo "  -l, --live      Run in live mode (default: dry-run)"
    echo "  -b, --batch     Batch size (default: 10)"
    echo "  -h, --help      Show this help message"
    echo ""
    echo "Examples:"
    echo "  # Dry run in development"
    echo "  $0 -e dev"
    echo ""
    echo "  # Live migration in production"
    echo "  $0 -e prod --live"
    echo ""
    echo "  # Custom batch size"
    echo "  $0 -e staging --batch 20"
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -e|--env)
            ENVIRONMENT="$2"
            shift 2
            ;;
        -o|--org)
            ORG_ID="$2"
            shift 2
            ;;
        -l|--live)
            DRY_RUN="false"
            shift
            ;;
        -b|--batch)
            BATCH_SIZE="$2"
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            echo "Unknown option $1"
            usage
            exit 1
            ;;
    esac
done

# Validate required arguments
if [[ -z "$ENVIRONMENT" ]]; then
    echo -e "${RED}Error: Environment is required${NC}"
    usage
    exit 1
fi

# Validate environment
case $ENVIRONMENT in
    dev|staging|prod)
        ;;
    *)
        echo -e "${RED}Error: Environment must be dev, staging, or prod${NC}"
        exit 1
        ;;
esac

# Set table names based on environment
# Table names follow terraform pattern: ${environment_name}-compute-resource-*-table-${region_shortname}
case $ENVIRONMENT in
    dev)
        NODES_TABLE="dev-compute-resource-nodes-table-use1"
        ACCESS_TABLE="dev-compute-node-access-table-use1"
        ;;
    staging)
        NODES_TABLE="staging-compute-resource-nodes-table-use1"
        ACCESS_TABLE="staging-compute-node-access-table-use1"
        ;;
    prod)
        NODES_TABLE="prod-compute-resource-nodes-table-use1"
        ACCESS_TABLE="prod-compute-node-access-table-use1"
        ;;
esac

# Allow overriding table names via environment variables for flexibility
if [[ -n "$NODES_TABLE_OVERRIDE" ]]; then
    NODES_TABLE="$NODES_TABLE_OVERRIDE"
    echo -e "${YELLOW}Using override for nodes table: $NODES_TABLE${NC}"
fi

if [[ -n "$ACCESS_TABLE_OVERRIDE" ]]; then
    ACCESS_TABLE="$ACCESS_TABLE_OVERRIDE"
    echo -e "${YELLOW}Using override for access table: $ACCESS_TABLE${NC}"
fi

# Display configuration
echo -e "${BLUE}=== Enable Organization Access Migration ===${NC}"
echo -e "Environment: ${GREEN}$ENVIRONMENT${NC}"
echo -e "Nodes Table: ${GREEN}$NODES_TABLE${NC}"
echo -e "Access Table: ${GREEN}$ACCESS_TABLE${NC}"
echo -e "Batch Size: ${GREEN}$BATCH_SIZE${NC}"
echo -e "Discover Organizations: ${GREEN}Automatically${NC}"

if [[ "$DRY_RUN" == "true" ]]; then
    echo -e "Mode: ${YELLOW}DRY RUN${NC}"
else
    echo -e "Mode: ${RED}LIVE MIGRATION${NC}"
fi

echo ""

# Production safety check
if [[ "$ENVIRONMENT" == "prod" && "$DRY_RUN" == "false" ]]; then
    echo -e "${RED}⚠️  WARNING: You are about to run a LIVE migration in PRODUCTION!${NC}"
    echo "This will modify production data."
    echo ""
    read -p "Are you absolutely sure you want to continue? (type 'YES' to confirm): " CONFIRM
    
    if [[ "$CONFIRM" != "YES" ]]; then
        echo "Migration cancelled."
        exit 0
    fi
fi

# Build command arguments
CMD_ARGS="-nodes-table $NODES_TABLE -access-table $ACCESS_TABLE -batch-size $BATCH_SIZE -env $ENVIRONMENT"

if [[ "$DRY_RUN" == "true" ]]; then
    CMD_ARGS="$CMD_ARGS -dry-run"
fi

# Change to script directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
cd "$SCRIPT_DIR"

# Run the migration
echo -e "${BLUE}Running migration command:${NC}"
echo "go run main.go $CMD_ARGS"
echo ""

go run main.go $CMD_ARGS

# Success message
if [[ $? -eq 0 ]]; then
    echo ""
    if [[ "$DRY_RUN" == "true" ]]; then
        echo -e "${GREEN}✅ Dry run completed successfully!${NC}"
        echo -e "${YELLOW}💡 Run with --live flag to execute the actual migration${NC}"
    else
        echo -e "${GREEN}🎉 Migration completed successfully!${NC}"
        echo -e "${BLUE}Organization members can now access the imported compute nodes${NC}"
    fi
else
    echo -e "${RED}❌ Migration failed. Check the logs above.${NC}"
    exit 1
fi