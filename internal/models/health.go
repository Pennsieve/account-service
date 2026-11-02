package models

const (
	HealthStatusHealthy     = "HEALTHY"
	HealthStatusWarning     = "WARNING"
	HealthStatusUnknown     = "UNKNOWN"
	HealthStatusUnreachable = "UNREACHABLE"
)

// HealthCheckResponse is the parsed response from a gateway's GET /health endpoint.
type HealthCheckResponse struct {
	Status    string               `json:"status"`
	Issues    []HealthCheckIssue   `json:"issues,omitempty"`
	Resources HealthCheckResources `json:"resources,omitempty"`
}

// HealthCheckResources contains resource inventories from the gateway.
type HealthCheckResources struct {
	// SFNActiveExecutions is the count of RUNNING workflow executions on the node
	// (workflows + interactive sessions). Used to gate node deletion. Omitted by
	// older gateways → treated as "unknown" (gate fails open to the provisioner backstop).
	SFNActiveExecutions int `json:"sfnActiveExecutions,omitempty"`
}

// HealthCheckIssue represents a single issue reported by the health endpoint.
type HealthCheckIssue struct {
	Component string `json:"component"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
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
