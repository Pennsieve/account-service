package store_postgres

import (
	"context"
	"database/sql"
	"fmt"
	
	_ "github.com/lib/pq"
)

type Team struct {
	Id             int64  `json:"id"`
	Name           string `json:"name"`
	NodeId         string `json:"nodeId"`
	OrganizationId int64  `json:"organizationId"`
}

type UserTeam struct {
	TeamId         int64  `json:"teamId"`
	TeamNodeId     string `json:"teamNodeId"`
	TeamName       string `json:"teamName"`
	UserId         int64  `json:"userId"`
	OrganizationId int64  `json:"organizationId"`
}

type TeamStore interface {
	GetUserTeams(ctx context.Context, userId, organizationId int64) ([]UserTeam, error)
	GetTeamById(ctx context.Context, teamId int64) (*Team, error)
	GetTeamMembers(ctx context.Context, teamId int64) ([]int64, error)
	GetTeamByNodeId(ctx context.Context, nodeId string) (*Team, error)
}

type PostgresTeamStore struct {
	DB *sql.DB
}

func NewPostgresTeamStore(db *sql.DB) TeamStore {
	return &PostgresTeamStore{DB: db}
}

// GetUserTeams returns all teams a user belongs to in an organization
func (s *PostgresTeamStore) GetUserTeams(ctx context.Context, userId, organizationId int64) ([]UserTeam, error) {
	query := `
		SELECT 
			t.id as team_id,
			t.node_id as team_node_id,
			t.name as team_name,
			tu.user_id,
			ot.organization_id
		FROM pennsieve.teams t
		JOIN pennsieve.team_user tu ON tu.team_id = t.id
		JOIN pennsieve.organization_team ot ON ot.team_id = t.id
		WHERE tu.user_id = $1 
		  AND ot.organization_id = $2`
	
	rows, err := s.DB.QueryContext(ctx, query, userId, organizationId)
	if err != nil {
		return nil, fmt.Errorf("error querying user teams: %w", err)
	}
	defer rows.Close()
	
	var teams []UserTeam
	for rows.Next() {
		var team UserTeam
		err = rows.Scan(
			&team.TeamId,
			&team.TeamNodeId,
			&team.TeamName,
			&team.UserId,
			&team.OrganizationId,
		)
		if err != nil {
			return nil, fmt.Errorf("error scanning team row: %w", err)
		}
		teams = append(teams, team)
	}
	
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating team rows: %w", err)
	}
	
	return teams, nil
}

// GetTeamById returns a team by its ID
func (s *PostgresTeamStore) GetTeamById(ctx context.Context, teamId int64) (*Team, error) {
	query := `
		SELECT 
			t.id,
			t.name,
			t.node_id,
			ot.organization_id
		FROM pennsieve.teams t
		JOIN pennsieve.organization_team ot ON ot.team_id = t.id
		WHERE t.id = $1`
	
	var team Team
	err := s.DB.QueryRowContext(ctx, query, teamId).Scan(
		&team.Id,
		&team.Name,
		&team.NodeId,
		&team.OrganizationId,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("error getting team: %w", err)
	}
	
	return &team, nil
}

// GetTeamMembers returns all user IDs that are members of a team
func (s *PostgresTeamStore) GetTeamMembers(ctx context.Context, teamId int64) ([]int64, error) {
	query := `
		SELECT user_id 
		FROM pennsieve.team_user 
		WHERE team_id = $1`
	
	rows, err := s.DB.QueryContext(ctx, query, teamId)
	if err != nil {
		return nil, fmt.Errorf("error querying team members: %w", err)
	}
	defer rows.Close()
	
	var userIds []int64
	for rows.Next() {
		var userId int64
		err = rows.Scan(&userId)
		if err != nil {
			return nil, fmt.Errorf("error scanning user id: %w", err)
		}
		userIds = append(userIds, userId)
	}
	
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating user rows: %w", err)
	}
	
	return userIds, nil
}

// GetTeamByNodeId returns a team by its node_id (e.g., "N:team:uuid")
func (s *PostgresTeamStore) GetTeamByNodeId(ctx context.Context, nodeId string) (*Team, error) {
	query := `
		SELECT 
			t.id,
			t.name,
			t.node_id,
			ot.organization_id
		FROM pennsieve.teams t
		JOIN pennsieve.organization_team ot ON ot.team_id = t.id
		WHERE t.node_id = $1`
	
	var team Team
	err := s.DB.QueryRowContext(ctx, query, nodeId).Scan(
		&team.Id,
		&team.Name,
		&team.NodeId,
		&team.OrganizationId,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("error getting team by node_id: %w", err)
	}
	
	return &team, nil
}