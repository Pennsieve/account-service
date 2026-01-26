package models

import (
	"time"
	
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// EntityType represents the type of entity that has access
type EntityType string

const (
	EntityTypeUser      EntityType = "user"
	EntityTypeTeam      EntityType = "team"
	EntityTypeWorkspace EntityType = "workspace"
)

// AccessType represents the type of access granted
type AccessType string

const (
	AccessTypeOwner  AccessType = "owner"
	AccessTypeShared AccessType = "shared"
	AccessTypeWorkspace AccessType = "workspace"
)

// NodeAccessScope represents the access scope level of a compute node
type NodeAccessScope string

const (
	AccessScopePrivate   NodeAccessScope = "private"
	AccessScopeWorkspace NodeAccessScope = "workspace"
	AccessScopeShared    NodeAccessScope = "shared"
)

// NodeAccess represents an access permission entry in DynamoDB
type NodeAccess struct {
	EntityId       string     `dynamodbav:"entityId"`                 // Format: "user#123" or "team#456" or "workspace#789"
	NodeId         string     `dynamodbav:"nodeId"`                   // Format: "node#uuid"
	EntityType     EntityType `dynamodbav:"entityType"`               // user, team, or workspace
	EntityRawId    string     `dynamodbav:"entityRawId"`              // The actual ID without prefix
	NodeUuid       string     `dynamodbav:"nodeUuid"`                 // The actual node UUID
	AccessType     AccessType `dynamodbav:"accessType"`               // owner, shared, or workspace
	OrganizationId string     `dynamodbav:"organizationId,omitempty"` // Organization ID (optional for organization-independent nodes)
	GrantedAt      time.Time  `dynamodbav:"grantedAt"`                // When access was granted
	GrantedBy      string     `dynamodbav:"grantedBy"`                // User ID who granted access
}

// GetKey returns the DynamoDB key for this access entry
func (n NodeAccess) GetKey() map[string]types.AttributeValue {
	entityId, _ := attributevalue.Marshal(n.EntityId)
	nodeId, _ := attributevalue.Marshal(n.NodeId)
	
	return map[string]types.AttributeValue{
		"entityId": entityId,
		"nodeId":   nodeId,
	}
}

// FormatEntityId creates a properly formatted entity ID
func FormatEntityId(entityType EntityType, id string) string {
	return string(entityType) + "#" + id
}

// FormatNodeId creates a properly formatted node ID
func FormatNodeId(uuid string) string {
	return "node#" + uuid
}

// NodeAccessRequest represents a request to grant/update node access
type NodeAccessRequest struct {
	NodeUuid        string          `json:"nodeUuid"`
	AccessScope     NodeAccessScope `json:"accessScope"`
	SharedWithUsers []string        `json:"sharedWithUsers,omitempty"`
	SharedWithTeams []string        `json:"sharedWithTeams,omitempty"`
}

// NodeAccessResponse represents the access settings for a node
type NodeAccessResponse struct {
	NodeUuid                 string          `json:"nodeUuid"`
	AccessScope              NodeAccessScope `json:"accessScope"`
	Owner                    string          `json:"owner"`
	SharedWithUsers          []string        `json:"sharedWithUsers,omitempty"`
	SharedWithTeams          []string        `json:"sharedWithTeams,omitempty"`
	OrganizationId           string          `json:"organizationId,omitempty"`
	OrganizationIndependent  bool            `json:"organizationIndependent"`
}

// IsOrganizationIndependent returns true if the node is not associated with any organization
func (n NodeAccess) IsOrganizationIndependent() bool {
	return n.OrganizationId == ""
}

// IsOrganizationIndependent returns true if the response represents an organization-independent node
func (r NodeAccessResponse) IsOrganizationIndependent() bool {
	return r.OrganizationId == ""
}

// ValidateAccessScope validates that the access scope is allowed for the node type
func (r NodeAccessRequest) ValidateForOrganizationIndependent() error {
	// Organization-independent nodes can only be private
	if r.AccessScope != AccessScopePrivate {
		return ErrOrganizationIndependentNodeCannotBeShared
	}
	
	// Cannot share with users or teams for organization-independent nodes
	if len(r.SharedWithUsers) > 0 || len(r.SharedWithTeams) > 0 {
		return ErrOrganizationIndependentNodeCannotBeShared
	}
	
	return nil
}

// NodeAttachmentRequest represents a request to attach a node to an organization
type NodeAttachmentRequest struct {
	NodeUuid       string `json:"nodeUuid"`
	OrganizationId string `json:"organizationId"`
}