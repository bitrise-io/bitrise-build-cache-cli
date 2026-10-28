package kv

import "context"

func (c *Client) ChangeSession(invocationID string, appSlug string, buildSlug string, stepSlug string) {
	c.sessionMutex.Lock()
	defer c.sessionMutex.Unlock()

	c.invocationID = invocationID
	c.cacheConfigMetadata.BitriseAppID = appSlug
	c.cacheConfigMetadata.BitriseBuildID = buildSlug
	c.cacheConfigMetadata.BitriseStepExecutionID = stepSlug
}

type invocationIDCtxKey struct{}

// WithInvocationID returns ctx carrying id as the per-call ToolInvocationId
// override. Empty id is a no-op (returns ctx unchanged). Used by the xcelerate
// proxy to tag per-peer gRPC call contexts with the peer's derived
// InvocationID without racing on the shared c.invocationID.
func WithInvocationID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}

	return context.WithValue(ctx, invocationIDCtxKey{}, id)
}

// InvocationIDFromContext returns the ctx-carried InvocationID, if any.
// Readers must short-circuit on ok=true BEFORE reading the shared
// c.invocationID — the latter is written by ChangeSession under
// sessionMutex but read without the lock at every CAS/KV call site.
func InvocationIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(invocationIDCtxKey{}).(string)

	return v, ok && v != ""
}
