package store_postgres

import (
	"context"
	"database/sql"
	"fmt"
	
	_ "github.com/lib/pq"
)

const (
	// Permission bit levels - 8 or higher means Collaborator or higher
	MinimumCollaboratorPermission = 8
	// Permission bit level for administrators (owners and admins)
	MinimumAdminPermission = 16
)

type OrganizationUser struct {
	UserId         int64 `json:"userId"`
	OrganizationId int64 `json:"organizationId"`
	PermissionBit  int   `json:"permissionBit"`
}

type OrganizationStore interface {
	// CheckUserOrganizationAccess checks if a user has at least Collaborator access (permission_bit >= 8) to an organization
	CheckUserOrganizationAccess(ctx context.Context, userId, organizationId int64) (bool, error)
	// GetUserPermissionBit returns the permission bit for a user in an organization
	GetUserPermissionBit(ctx context.Context, userId, organizationId int64) (int, error)
	// CheckUserIsOrganizationAdmin checks if a user has admin access (permission_bit >= 16) to an organization
	CheckUserIsOrganizationAdmin(ctx context.Context, userId, organizationId int64) (bool, error)
	// CheckUserExists checks if a user exists in the platform
	CheckUserExists(ctx context.Context, userId int64) (bool, error)
}

type PostgresOrganizationStore struct {
	DB *sql.DB
}

func NewPostgresOrganizationStore(db *sql.DB) OrganizationStore {
	return &PostgresOrganizationStore{DB: db}
}

// CheckUserOrganizationAccess checks if a user has at least Collaborator access (permission_bit >= 8) to an organization
func (s *PostgresOrganizationStore) CheckUserOrganizationAccess(ctx context.Context, userId, organizationId int64) (bool, error) {
	query := `
		SELECT permission_bit 
		FROM pennsieve.organization_user 
		WHERE user_id = $1 AND organization_id = $2`
	
	var permissionBit int
	err := s.DB.QueryRowContext(ctx, query, userId, organizationId).Scan(&permissionBit)
	if err != nil {
		if err == sql.ErrNoRows {
			// User is not a member of the organization
			return false, nil
		}
		return false, fmt.Errorf("error checking user organization access: %w", err)
	}
	
	// Check if user has Collaborator or higher permissions (permission_bit >= 8)
	return permissionBit >= MinimumCollaboratorPermission, nil
}

// GetUserPermissionBit returns the permission bit for a user in an organization
func (s *PostgresOrganizationStore) GetUserPermissionBit(ctx context.Context, userId, organizationId int64) (int, error) {
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
	permissionBit, err := s.GetUserPermissionBit(ctx, userId, organizationId)
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

// CheckUserExists checks if a user exists in the platform
func (s *PostgresOrganizationStore) CheckUserExists(ctx context.Context, userId int64) (bool, error) {
	query := `
		SELECT 1 
		FROM pennsieve.users 
		WHERE id = $1`
	
	var exists int
	err := s.DB.QueryRowContext(ctx, query, userId).Scan(&exists)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("error checking if user exists: %w", err)
	}
	
	return true, nil
}