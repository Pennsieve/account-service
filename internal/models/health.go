package models

const (
	HealthStatusHealthy     = "HEALTHY"
	HealthStatusWarning     = "WARNING"
	HealthStatusUnknown     = "UNKNOWN"
	HealthStatusUnreachable = "UNREACHABLE"
)

// HealthCheckResponse is the parsed response from a gateway's GET /health endpoint.
type HealthCheckResponse struct {
	Status    string                `json:"status"`
	Issues    []HealthCheckIssue    `json:"issues,omitempty"`
	Resources HealthCheckResources  `json:"resources,omitempty"`
}

// HealthCheckResources contains resource inventories from the gateway.
type HealthCheckResources struct {
	EFSLayers []EFSLayerInfo `json:"efsLayers,omitempty"`
}

// EFSLayerInfo describes a layer on EFS as reported by the gateway health check.
type EFSLayerInfo struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"sizeBytes"`
	FileCount int    `json:"fileCount"`
}

// HealthCheckIssue represents a single issue reported by the health endpoint.
type HealthCheckIssue struct {
	Component string `json:"component"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
}

// LayerRecord mirrors the workflow-service compute-node-layers DynamoDB schema.
type LayerRecord struct {
	ComputeNodeId string `dynamodbav:"computeNodeId"`
	LayerName     string `dynamodbav:"layerName"`
	Status        string `dynamodbav:"status"`
	SizeBytes     int64  `dynamodbav:"sizeBytes"`
	FileCount     int    `dynamodbav:"fileCount"`
	Description   string `dynamodbav:"description,omitempty"`
	CreatedAt     string `dynamodbav:"createdAt"`
	CreatedBy     string `dynamodbav:"createdBy"`
	LastAccessed  string `dynamodbav:"lastAccessed,omitempty"`
}

// DynamoDBHealthCheckLog is the DynamoDB record for a health check log entry.
type DynamoDBHealthCheckLog struct {
	NodeId       string `dynamodbav:"nodeId"`
	Timestamp    string `dynamodbav:"timestamp"`
	Status       string `dynamodbav:"status"`
	Issues       string `dynamodbav:"issues,omitempty"`
	ResponseBody string `dynamodbav:"responseBody,omitempty"`
	TTL          int64  `dynamodbav:"ttl"`
}