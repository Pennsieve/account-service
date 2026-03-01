package handler

import (
    "context"
    "log/slog"

    "github.com/aws/aws-lambda-go/events"
    "github.com/aws/aws-lambda-go/lambdacontext"
    accountHandler "github.com/pennsieve/account-service/internal/handler/account"
    computeHandler "github.com/pennsieve/account-service/internal/handler/compute"
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
    router.GET("/pennsieve-accounts/{accountType}", accountHandler.GetPennsieveAccountsHandler)
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

    // Role policy endpoint
    router.GET("/role-policy", computeHandler.GetRolePolicyHandler)

    return router.Start(ctx, request)
}
