package handler

import (
    "context"
    "log/slog"

    "github.com/aws/aws-lambda-go/events"
    "github.com/aws/aws-lambda-go/lambdacontext"
    accountHandler "github.com/pennsieve/account-service/internal/handler/account"
    computeHandler "github.com/pennsieve/account-service/internal/handler/compute"
    storageHandler "github.com/pennsieve/account-service/internal/handler/storage"
    "github.com/pennsieve/account-service/internal/logging"
)

var logger = logging.Default

func init() {
    logger.Info("init()")
}

func AccountServiceHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
    if lc, ok := lambdacontext.FromContext(ctx); ok {
        logger = logger.With(slog.String("requestID", lc.AwsRequestID))
    }

    router := NewLambdaRouter()
    // Account management routes
    router.POST("/accounts", accountHandler.PostAccountsHandler)
    router.GET("/accounts", accountHandler.GetAccountsHandler)
    router.GET("/accounts/{id}", accountHandler.GetAccountHandler)
    router.PATCH("/accounts/{id}", accountHandler.PatchAccountHandler)
    router.DELETE("/accounts/{id}", accountHandler.DeleteAccountHandler)

    // Workspace Enabling Routes
    router.POST("/accounts/{uuid}/workspaces", accountHandler.PostAccountWorkspaceEnablementHandler)
    router.DELETE("/accounts/{uuid}/workspaces/{workspaceId}", accountHandler.DeleteAccountWorkspaceEnablementHandler)

    // Compute node routes
    router.POST("/compute-nodes", computeHandler.PostComputeNodesHandler)
    router.GET("/compute-nodes", computeHandler.GetComputesNodesHandler)
    router.GET("/compute-nodes/{id}", computeHandler.GetComputeNodeHandler)
    router.PUT("/compute-nodes/{id}", computeHandler.PutComputeNodeHandler)
    router.PATCH("/compute-nodes/{id}", computeHandler.PatchComputeNodeHandler)
    router.DELETE("/compute-nodes/{id}", computeHandler.DeleteComputeNodeHandler)
    router.POST("/compute-nodes/{id}/update-config", computeHandler.PostUpdateConfigHandler)

    // Compute node permission routes
    router.GET("/compute-nodes/{id}/permissions", computeHandler.GetNodePermissionsHandler)
    router.PUT("/compute-nodes/{id}/permissions", computeHandler.SetNodeAccessScopeHandler)
    router.POST("/compute-nodes/{id}/permissions/users", computeHandler.GrantUserAccessHandler)
    router.DELETE("/compute-nodes/{id}/permissions/users/{userId}", computeHandler.RevokeUserAccessHandler)
    router.POST("/compute-nodes/{id}/permissions/teams", computeHandler.GrantTeamAccessHandler)
    router.DELETE("/compute-nodes/{id}/permissions/teams/{teamId}", computeHandler.RevokeTeamAccessHandler)
    
    // Organization attachment/detachment routes
    router.POST("/compute-nodes/{id}/organization", computeHandler.AttachNodeToOrganizationHandler)
    router.DELETE("/compute-nodes/{id}/organization", computeHandler.DetachNodeFromOrganizationHandler)

    // Compute node secrets routes (user secrets)
    router.PATCH("/compute-nodes/{id}/secrets", computeHandler.PutSecretsHandler)
    router.GET("/compute-nodes/{id}/secrets", computeHandler.GetSecretsHandler)
    router.DELETE("/compute-nodes/{id}/secrets", computeHandler.DeleteSecretsHandler)

    // Compute node secrets routes (shared secrets — owner only for PATCH/DELETE)
    router.PATCH("/compute-nodes/{id}/shared-secrets", computeHandler.PutSharedSecretsHandler)
    router.GET("/compute-nodes/{id}/shared-secrets", computeHandler.GetSharedSecretsHandler)
    router.DELETE("/compute-nodes/{id}/shared-secrets", computeHandler.DeleteSharedSecretsHandler)

    // Compute node allowed processors (whitelist — owner only for PUT)
    router.PUT("/compute-nodes/{id}/allowed-processors", computeHandler.PutAllowedProcessorsHandler)
    router.GET("/compute-nodes/{id}/allowed-processors", computeHandler.GetAllowedProcessorsHandler)

    // Node-wide LLM cost budget (the SSM-backed cap the governor enforces).
    // Owner-only. Distinct from the per-user chat quotas below: this cap
    // applies to *every* LLM caller on the node — chat *and* workflow
    // applications. It's the only backstop for application-initiated runs
    // since those don't go through chat-service's CheckTurn.
    router.PUT("/compute-nodes/{id}/llm-config", computeHandler.PutLLMConfigHandler)
    router.GET("/compute-nodes/{id}/llm-config", computeHandler.GetLLMConfigHandler)

    // Per-user chat & workflow LLM quotas. Owner-only for PUT/DELETE/list and
    // for reads of the `__default__` row. GET of a specific user's row +
    // /effective + /user-usage allow the user themselves (or "me" alias).
    router.GET("/compute-nodes/{id}/user-quotas", computeHandler.ListChatUserQuotasHandler)
    router.PUT("/compute-nodes/{id}/user-quotas/{userId}", computeHandler.PutChatUserQuotaHandler)
    router.GET("/compute-nodes/{id}/user-quotas/{userId}", computeHandler.GetChatUserQuotaHandler)
    router.DELETE("/compute-nodes/{id}/user-quotas/{userId}", computeHandler.DeleteChatUserQuotaHandler)
    router.GET("/compute-nodes/{id}/user-quotas/{userId}/effective", computeHandler.GetChatUserEffectiveQuotaHandler)
    router.GET("/compute-nodes/{id}/user-usage/{userId}", computeHandler.GetChatUserUsageHandler)

    // App Store access endpoint
    router.POST("/app-store/access", computeHandler.PostAppStoreAccessHandler)

    // GPU tier reference endpoint
    router.GET("/gpu-tiers", computeHandler.GetGPUTiersHandler)

    // Role policy endpoint
    router.GET("/role-policy", computeHandler.GetRolePolicyHandler)

    // Storage node routes
    router.POST("/storage-nodes", storageHandler.PostStorageNodeHandler)
    router.GET("/storage-nodes", storageHandler.GetStorageNodesHandler)
    router.GET("/storage-nodes/{id}", storageHandler.GetStorageNodeHandler)
    router.PATCH("/storage-nodes/{id}", storageHandler.PatchStorageNodeHandler)
    router.DELETE("/storage-nodes/{id}", storageHandler.DeleteStorageNodeHandler)
    router.POST("/storage-nodes/{id}/workspace", storageHandler.AttachToWorkspaceHandler)
    router.DELETE("/storage-nodes/{id}/workspace", storageHandler.DetachFromWorkspaceHandler)
    router.POST("/storage-nodes/{id}/update-config", storageHandler.PostUpdateConfigHandler)
    router.GET("/storage-nodes/{id}/impact", storageHandler.GetDetachImpactHandler)

    return router.Start(ctx, request)
}
