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

type TeamStore interface {
	GetTeamByNodeId(ctx context.Context, nodeId string) (*Team, error)
}

type PostgresTeamStore struct {
	DB *sql.DB
}

func NewPostgresTeamStore(db *sql.DB) TeamStore {
	return &PostgresTeamStore{DB: db}
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