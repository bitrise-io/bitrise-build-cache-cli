package proxy

import (
	"context"

	"google.golang.org/grpc/metadata"
)

// workspaceIDMetadataKey is the gRPC outgoing/incoming metadata key that
// carries the marker-resolved workspace slug from wrapper → proxy → kv session.
const workspaceIDMetadataKey = "x-bitrise-workspace-id"

// ContextWithWorkspaceID returns a ctx that carries workspaceID as outgoing
// gRPC metadata so the receiving proxy can pick it up on SetSession. Empty
// slug is a no-op.
func ContextWithWorkspaceID(ctx context.Context, workspaceID string) context.Context {
	if workspaceID == "" {
		return ctx
	}

	return metadata.AppendToOutgoingContext(ctx, workspaceIDMetadataKey, workspaceID)
}

// WorkspaceIDFromContext extracts the workspace slug from incoming gRPC
// metadata. Empty when the key is absent.
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
