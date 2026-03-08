package store_postgres

import (
	"context"
	"database/sql"
	"fmt"
	
	_ "github.com/lib/pq"
)

const (
	// Permission bit level for administrators (owners and admins)
	MinimumAdminPermission = 16
)

type OrganizationStore interface {
	// CheckUserIsOrganizationAdmin checks if a user has admin access (permission_bit >= 16) to an organization
	CheckUserIsOrganizationAdmin(ctx context.Context, userId, organizationId int64) (bool, error)
	// CheckUserExistsByNodeId checks if a user exists by their node_id (e.g., "N:user:uuid")
	CheckUserExistsByNodeId(ctx context.Context, nodeId string) (bool, error)
	// GetOrganizationIdByNodeId returns the numeric organization ID for a given node_id
	GetOrganizationIdByNodeId(ctx context.Context, nodeId string) (int64, error)
}

type PostgresOrganizationStore struct {
	DB *sql.DB
}

func NewPostgresOrganizationStore(db *sql.DB) OrganizationStore {
	return &PostgresOrganizationStore{DB: db}
}

// getUserPermissionBit returns the permission bit for a user in an organization
func (s *PostgresOrganizationStore) getUserPermissionBit(ctx context.Context, userId, organizationId int64) (int, error) {
	query := `
		SELECT permission_bit 
		FROM pennsieve.organization_user 
		WHERE user_id = $1 AND organization_id = $2`
	
	var permissionBit int
	err := s.DB.QueryRowContext(ctx, query, userId, organizationId).Scan(&permissionBit)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("user %d is not a member of organization %d", userId, organizationId)
		}
		return 0, fmt.Errorf("error getting user permission bit: %w", err)
	}
	
	return permissionBit, nil
}

// CheckUserIsOrganizationAdmin checks if a user has admin access (permission_bit >= 16) to an organization
func (s *PostgresOrganizationStore) CheckUserIsOrganizationAdmin(ctx context.Context, userId, organizationId int64) (bool, error) {
	permissionBit, err := s.getUserPermissionBit(ctx, userId, organizationId)
	if err != nil {
		// If user is not a member, return false without error
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	
	// Check if user has Admin or Owner permissions (permission_bit >= 16)
	return permissionBit >= MinimumAdminPermission, nil
}

// CheckUserExistsByNodeId checks if a user exists by their node_id (e.g., "N:user:uuid")
func (s *PostgresOrganizationStore) CheckUserExistsByNodeId(ctx context.Context, nodeId string) (bool, error) {
	query := `
		SELECT 1 
		FROM pennsieve.users 
		WHERE node_id = $1`
	
	var exists int
	err := s.DB.QueryRowContext(ctx, query, nodeId).Scan(&exists)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("error checking if user exists by node_id: %w", err)
	}
	
	return true, nil
}

// GetOrganizationIdByNodeId returns the numeric organization ID for a given node_id
func (s *PostgresOrganizationStore) GetOrganizationIdByNodeId(ctx context.Context, nodeId string) (int64, error) {
	query := `
		SELECT id 
		FROM pennsieve.organizations 
		WHERE node_id = $1`
	
	var orgId int64
	err := s.DB.QueryRowContext(ctx, query, nodeId).Scan(&orgId)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("organization with node_id %s not found", nodeId)
		}
		return 0, fmt.Errorf("error getting organization ID by node_id: %w", err)
	}
	
	return orgId, nil
}