package proxy

import (
	"context"

	"google.golang.org/grpc/metadata"
)

const workspaceIDMetadataKey = "x-bitrise-workspace-id"

func ContextWithWorkspaceID(ctx context.Context, workspaceID string) context.Context {
	if workspaceID == "" {
		return ctx
	}

	return metadata.AppendToOutgoingContext(ctx, workspaceIDMetadataKey, workspaceID)
}

func WorkspaceIDFromContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}

	values := md.Get(workspaceIDMetadataKey)
	if len(values) == 0 {
		return ""
	}

	return values[0]
}
