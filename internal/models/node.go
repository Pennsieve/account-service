package models

import (
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/pennsieve/pennsieve-go-core/pkg/models/tenant"
)

// CreateComputeNodeRequest is the request body for POST /compute-nodes
type CreateComputeNodeRequest struct {
	AccountId           string  `json:"accountId"`
	Name                string  `json:"name"`
	Description         string  `json:"description"`
	OrganizationId      string  `json:"organizationId,omitempty"`
	ProvisionerImage    string  `json:"provisionerImage,omitempty"`
	ProvisionerImageTag string  `json:"provisionerImageTag,omitempty"`
	DeploymentMode      string  `json:"deploymentMode,omitempty"`
	EnableLLMAccess     bool    `json:"enableLLMAccess,omitempty"`
	LlmBaaAcknowledged  bool    `json:"llmBaaAcknowledged,omitempty"`
	MaxGpuInstances     *int    `json:"maxGpuInstances,omitempty"`
	GpuTier             *string `json:"gpuTier,omitempty"`
	// MaxInteractiveSessions > 0 enables interactive (Jupyter) sessions on the
	// node and drives the shared per-account ALB + DNS delegation.
	MaxInteractiveSessions *int `json:"maxInteractiveSessions,omitempty"`
}

// Node is the response representation of a compute node
type Node struct {
	Uuid                   string          `json:"uuid"`
	Name                   string          `json:"name"`
	Description            string          `json:"description"`
	QueueUrl               string          `json:"queueUrl"`
	Account                NodeAccount     `json:"account"`
	CreatedAt              string          `json:"createdAt"`
	OrganizationId         string          `json:"organizationId,omitempty"`
	OwnerId                string          `json:"ownerId"`
	Identifier             string          `json:"identifier"`
	WorkflowManagerTag     string          `json:"workflowManagerTag"`
	ProvisionerImage       string          `json:"provisionerImage,omitempty"`
	ProvisionerImageTag    string          `json:"provisionerImageTag,omitempty"`
	DeploymentMode         string          `json:"deploymentMode,omitempty"`
	EnableLLMAccess        bool            `json:"enableLLMAccess,omitempty"`
	LlmBaaAcknowledged     bool            `json:"llmBaaAcknowledged,omitempty"`
	MaxGpuInstances        int             `json:"maxGpuInstances"`
	GpuTier                string          `json:"gpuTier"`
	MaxInteractiveSessions int             `json:"maxInteractiveSessions"`
	AccessScope            NodeAccessScope `json:"accessScope,omitempty"`
	Status                 string          `json:"status"`
	HealthStatus           string          `json:"healthStatus,omitempty"`
	LastHealthCheck        string          `json:"lastHealthCheck,omitempty"`
	// LatestVersion is the newest real vX.Y.Z provisioner image tag available on
	// Docker Hub for this node's ProvisionerImage. Empty when it can't be
	// determined (Docker Hub unreachable, no released versions, etc.).
	LatestVersion string `json:"latestVersion,omitempty"`
	// UpdateAvailable is true when the node's provisioner tag is behind
	// LatestVersion (or is a floating tag like "latest"). Always false when
	// LatestVersion is unknown.
	UpdateAvailable bool `json:"updateAvailable"`
}

// NodesListResponse is the body of GET /compute-nodes. LatestVersion is the
// newest released provisioner tag for the default provisioner image, reported
// at the root so callers can pin a new node correctly even when the list is
// empty. Empty when it can't be determined.
type NodesListResponse struct {
	Nodes         []Node `json:"nodes"`
	LatestVersion string `json:"latestVersion,omitempty"`
}

type NodeAccount struct {
	Uuid        string `json:"uuid"`
	AccountId   string `json:"accountId"`
	AccountType string `json:"accountType"`
	OwnerId     string `json:"ownerId,omitempty"` // The owner of the AWS/GCP account
}

type NodeResponse struct {
	Message string `json:"message"`
}

type NodeUpdateRequest struct {
	WorkflowManagerTag    string `json:"workflowManagerTag,omitempty"`    // Optional - only for legacy provisioner
	WorkflowManagerCpu    int    `json:"workflowManagerCpu,omitempty"`    // Optional - only for legacy provisioner
	WorkflowManagerMemory int    `json:"workflowManagerMemory,omitempty"` // Optional - only for legacy provisioner
	AuthorizationType     string `json:"authorizationType,omitempty"`     // Optional - "NONE" or "AWS_IAM", only for legacy provisioner
	ProvisionerImage      string `json:"provisionerImage,omitempty"`      // Optional - Docker image for the provisioner
	ProvisionerImageTag   string `json:"provisionerImageTag,omitempty"`   // Optional - Docker tag for the provisioner image
	// MaxInteractiveSessions, when set, enables/disables interactive (Jupyter)
	// sessions on an existing node (>0 enables). Pointer so "absent" means "leave
	// unchanged" vs. an explicit 0 to disable.
	MaxInteractiveSessions *int `json:"maxInteractiveSessions,omitempty"`
}

// DynamoDBNode represents the DynamoDB storage structure for compute nodes
type DynamoDBNode struct {
	Uuid                  string `dynamodbav:"uuid"`
	Name                  string `dynamodbav:"name"`
	Description           string `dynamodbav:"description"`
	ComputeNodeGatewayUrl string `dynamodbav:"computeNodeGatewayUrl"`
	EfsId                 string `dynamodbav:"efsId"`
	QueueUrl              string `dynamodbav:"queueUrl"`
	Env                   string `dynamodbav:"environment"`
	AccountUuid           string `dynamodbav:"accountUuid"`
	AccountId             string `dynamodbav:"accountId"`
	AccountType           string `dynamodbav:"accountType"`
	CreatedAt             string `dynamodbav:"createdAt"`
	// Tenant owns, and pays for, the node: its organization, or the user who
	// created it when it has none. It is the source of truth for both.
	Tenant tenant.Tenant `dynamodbav:"tenant"`
	// OrganizationId is derived from Tenant when the node is stored (see
	// LegacyOrganizationId); read the organization with OrganizationNodeId.
	OrganizationId         string `dynamodbav:"organizationId"`
	UserId                 string `dynamodbav:"userId"`
	Identifier             string `dynamodbav:"identifier"`
	WorkflowManagerTag     string `dynamodbav:"workflowManagerTag"`
	DeploymentMode         string `dynamodbav:"deploymentMode"`
	ProvisionerImage       string `dynamodbav:"provisionerImage"`
	ProvisionerImageTag    string `dynamodbav:"provisionerImageTag"`
	EnableLLMAccess        bool   `dynamodbav:"enableLLMAccess"`
	LlmBaaAcknowledged     bool   `dynamodbav:"llmBaaAcknowledged"`
	MaxGpuInstances        int    `dynamodbav:"maxGpuInstances"`
	GpuTier                string `dynamodbav:"gpuTier"`
	MaxInteractiveSessions int    `dynamodbav:"maxInteractiveSessions"`
	Status                 string `dynamodbav:"status"`
	HealthStatus           string `dynamodbav:"healthStatus"`
	LastHealthCheck        string `dynamodbav:"lastHealthCheck"`
}

func (i DynamoDBNode) GetKey() map[string]types.AttributeValue {
	uuid, err := attributevalue.Marshal(i.Uuid)
	if err != nil {
		panic(err)
	}

	return map[string]types.AttributeValue{"uuid": uuid}
}

// legacyUserOwnedOrganizationId is the organizationId stored for a node a
// user owns. workflow-service and the provisioner read that value, so it is
// written until they read the tenant instead.
const legacyUserOwnedOrganizationId = "INDEPENDENT"

// NodeTenant is the tenant of a node created for organizationId (an
// organization node id), or for userId (a user node id) when organizationId
// is empty.
func NodeTenant(organizationId, userId string) (tenant.Tenant, error) {
	if organizationId != "" {
		return tenant.FromOrgNodeId(organizationId)
	}
	return tenant.FromUserNodeId(userId)
}

// LegacyOrganizationId is the organizationId stored, and passed to the
// provisioner, for a node with tenant t.
func LegacyOrganizationId(t tenant.Tenant) string {
	return tenant.Match(t,
		func(orgNodeId string) string { return orgNodeId },
		func(string) string { return legacyUserOwnedOrganizationId },
	)
}

// OrganizationFromLegacy reads an organizationId that may hold the legacy
// value for a user-owned node, as callers outside this service still send:
// the organization node id, or "" when there is none.
func OrganizationFromLegacy(organizationId string) string {
	if organizationId == legacyUserOwnedOrganizationId {
		return ""
	}
	return organizationId
}

// WithTenant returns n with its tenant set from organizationId and userId
// when it predates tenants.
func (n DynamoDBNode) WithTenant() (DynamoDBNode, error) {
	if !n.Tenant.IsZero() {
		return n, nil
	}
	t, err := NodeTenant(OrganizationFromLegacy(n.OrganizationId), n.UserId)
	if err != nil {
		return n, err
	}
	n.Tenant = t
	return n, nil
}

// OrganizationNodeId is the node's organization, or "" when a user owns it
// (or, for a record without a tenant, when it has none).
func (n DynamoDBNode) OrganizationNodeId() string {
	if n.Tenant.IsZero() {
		return OrganizationFromLegacy(n.OrganizationId)
	}
	return tenant.Match(n.Tenant,
		func(orgNodeId string) string { return orgNodeId },
		func(string) string { return "" },
	)
}
