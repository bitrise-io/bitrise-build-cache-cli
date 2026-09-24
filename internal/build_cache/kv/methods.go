package kv

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/retry"
	"github.com/dustin/go-humanize"
	"google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	remoteexecution "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/build/bazel/remote/execution/v2"
)

type PutParams struct {
	Name            string
	Sha256Sum       string
	FileSize        int64
	Offset          int64
	DeleteOnRewrite bool
}

type WriteStatus struct {
	Complete      bool
	CommittedSize int64
}

type FileDigest struct {
	Sha256Sum   string
	SizeInBytes int64
}

func (c *Client) GetCapabilities(ctx context.Context) error {
	if c.authBroken(ctx) {
		return ErrCacheUnauthenticated
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	ch := c.pickChannel()
	if err := c.acquireOn(timeoutCtx, ch); err != nil {
		return err
	}
	defer ch.release()

	callCtx := metadata.NewOutgoingContext(timeoutCtx, c.getMethodCallMetadata(ctx, true))

	_, err := ch.capabilitiesClient.GetCapabilities(callCtx, &remoteexecution.GetCapabilitiesRequest{})
	if err != nil {
		if c.tripAuth(ctx, err, sentAuth(callCtx)) {
			return ErrCacheUnauthenticated
		}

		return fmt.Errorf("get capabilities: %w", err)
	}

	return nil
}

func (c *Client) GetCapabilitiesWithRetry(ctx context.Context) error {
	//nolint:wrapcheck
	return retry.Times(10).Wait(3 * time.Second).TryWithAbort(func(attempt uint) (error, bool) {
		if attempt > 0 {
			c.logger.Debugf("Retrying GetCapabilities... (attempt %d)", attempt)
		}

		if err := c.GetCapabilities(ctx); err != nil {
			// The gate already warned once per rejected credential.
			if errors.Is(err, ErrCacheUnauthenticated) {
				return ErrCacheUnauthenticated, true
			}
			c.logger.Errorf("Error in GetCapabilities attempt %d: %s", attempt, err)

			return err, false
		}

		return nil, false
	})
}

func (c *Client) initiatePut(ctx context.Context, params PutParams) (*writer, error) {
	if c.authBroken(ctx) {
		return nil, ErrCacheUnauthenticated
	}

	md := metadata.Join(c.getMethodCallMetadata(ctx, false), metadata.Pairs(
		"x-flare-blob-validation-sha256", params.Sha256Sum,
		"x-flare-blob-validation-level", "error",
		"x-flare-no-skip-duplicate-writes", "true",
	))
	if params.DeleteOnRewrite {
		md.Set("x-cache-delete-on-rewrite", "true")
	}
	// Timeout is the responsibility of the caller
	ctx = metadata.NewOutgoingContext(ctx, md)

	ch := c.pickChannel()
	if err := c.acquireOn(ctx, ch); err != nil {
		return nil, err
	}

	stream, err := ch.bitriseKVClient.Put(ctx)
	if err != nil {
		ch.release()

		if c.tripAuth(ctx, err, sentAuth(ctx)) {
			return nil, ErrCacheUnauthenticated
		}

		return nil, fmt.Errorf("initiate put: %w", err)
	}

	resourceName := fmt.Sprintf("kv/%s", params.Name)

	w := &writer{
		auth:         sentAuth(ctx),
		stream:       stream,
		resourceName: resourceName,
		offset:       params.Offset,
		fileSize:     params.FileSize,
		release:      ch.release,
	}

	return w, nil
}

func (c *Client) initiateGet(ctx context.Context, logger log.Logger, name string, offset int64) (*reader, error) {
	if c.authBroken(ctx) {
		return nil, ErrCacheUnauthenticated
	}

	resourceName := fmt.Sprintf("kv/%s", name)

	// Timeout is the responsibility of the caller
	ctx = metadata.NewOutgoingContext(ctx, c.getMethodCallMetadata(ctx, false))

	readReq := &bytestream.ReadRequest{
		ResourceName: resourceName,
		ReadOffset:   offset,
		ReadLimit:    0,
	}

	ch := c.pickChannel()
	if err := c.acquireOn(ctx, ch); err != nil {
		return nil, err
	}

	stream, err := ch.bitriseKVClient.Get(ctx, readReq)
	if err != nil {
		ch.release()

		if c.tripAuth(ctx, err, sentAuth(ctx)) {
			return nil, ErrCacheUnauthenticated
		}

		return nil, fmt.Errorf("initiate get: %w", err)
	}

	r := &reader{
		auth:          sentAuth(ctx),
		logger:        logger,
		stream:        stream,
		metadataReady: make(chan struct{}),
		release:       ch.release,
	}
	go r.readStreamMetadata()

	return r, nil
}

func (c *Client) Delete(ctx context.Context, name string) error {
	if c.authBroken(ctx) {
		return ErrCacheUnauthenticated
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	ch := c.pickChannel()
	if err := c.acquireOn(timeoutCtx, ch); err != nil {
		return err
	}
	defer ch.release()

	resourceName := fmt.Sprintf("kv/%s", name)

	callCtx := metadata.NewOutgoingContext(timeoutCtx, c.getMethodCallMetadata(ctx, false))

	readReq := &bytestream.ReadRequest{
		ResourceName: resourceName,
		ReadOffset:   0,
		ReadLimit:    0,
	}
	_, err := ch.bitriseKVClient.Delete(callCtx, readReq)
	if err != nil {
		if c.tripAuth(ctx, err, sentAuth(callCtx)) {
			return ErrCacheUnauthenticated
		}

		return fmt.Errorf("initiate delete: %w", err)
	}

	return nil
}

func (c *Client) findMissing(ctx context.Context,
	req *remoteexecution.FindMissingBlobsRequest,
) ([]*FileDigest, error) {
	if c.authBroken(ctx) {
		return nil, ErrCacheUnauthenticated
	}

	var resp *remoteexecution.FindMissingBlobsResponse
	err := retry.Times(3).Wait(3 * time.Second).TryWithAbort(func(attempt uint) (error, bool) {
		if attempt > 0 {
			c.logger.Debugf("Retrying FindMissingBlobs... (attempt %d)", attempt)
		}

		timeoutCtx, cancel := context.WithTimeout(ctx, 20*time.Second)

		ch := c.pickChannel()
		if err := c.acquireOn(timeoutCtx, ch); err != nil {
			cancel()

			return err, true
		}

		callCtx := metadata.NewOutgoingContext(timeoutCtx, c.getMethodCallMetadata(ctx, false))

		var err error
		resp, err = ch.casClient.FindMissingBlobs(callCtx, req)

		cancel()
		ch.release()

		if err != nil {
			c.logger.Errorf("Error in FindMissingBlobs attempt %d: %s", attempt, err)

			// A rejected token is never accepted on a retry; anything else may be
			// transient and is worth another attempt.
			if c.tripAuth(ctx, err, sentAuth(callCtx)) {
				return ErrCacheUnauthenticated, true
			}

			return fmt.Errorf("find missing blobs: %w", err), false
		}

		return nil, false
	})
	if err != nil {
		return nil, fmt.Errorf("with retries: %w", err)
	}

	return convertToFileDigests(resp.GetMissingBlobDigests()), nil
}

const maxDigestsPerFindMissingRequest = 1000

func (c *Client) FindMissing(ctx context.Context, digests []*FileDigest) ([]*FileDigest, error) {
	if len(digests) == 0 {
		return nil, nil
	}

	blobDigests := convertToBlobDigests(digests)

	if len(digests) <= maxDigestsPerFindMissingRequest {
		req := &remoteexecution.FindMissingBlobsRequest{BlobDigests: blobDigests}
		c.logger.Debugf("Size of FindMissingBlobs request for %d blobs is %s", len(digests), humanize.Bytes(uint64(len(req.String()))))

		return c.findMissing(ctx, req)
	}

	var missingBlobs []*FileDigest
	for startIndex := 0; startIndex < len(digests); startIndex += maxDigestsPerFindMissingRequest {
		endIndex := min(startIndex+maxDigestsPerFindMissingRequest, len(digests))
		req := &remoteexecution.FindMissingBlobsRequest{BlobDigests: blobDigests[startIndex:endIndex]}
		c.logger.Debugf("Calling FindMissingBlobs for chunk: digests[%d:%d]", startIndex, endIndex)

		resp, err := c.findMissing(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("find missing blobs: %w", err)
		}

		missingBlobs = append(missingBlobs, resp...)
	}

	return missingBlobs, nil
}

func convertToBlobDigests(digests []*FileDigest) []*remoteexecution.Digest {
	out := make([]*remoteexecution.Digest, 0, len(digests))

	for _, d := range digests {
		out = append(out, &remoteexecution.Digest{
			Hash:      d.Sha256Sum,
			SizeBytes: d.SizeInBytes,
		})
	}

	return out
}

func convertToFileDigests(digests []*remoteexecution.Digest) []*FileDigest {
	out := make([]*FileDigest, 0, len(digests))

	for _, d := range digests {
		out = append(out, &FileDigest{
			Sha256Sum:   d.GetHash(),
			SizeInBytes: d.GetSizeBytes(),
		})
	}

	return out
}

func bearer(token string) string { return "bearer " + token }

// currentAuth yields the header the next RPC would send, nil without an AuthSource.
func (c *Client) currentAuth(ctx context.Context) func() string {
	if c.authSource == nil {
		return nil
	}

	return func() string { return bearer(c.authSource.Get(ctx).Token) }
}

func (c *Client) authBroken(ctx context.Context) bool {
	return c.authGate.isBroken(c.currentAuth(ctx))
}

func (c *Client) tripAuth(ctx context.Context, err error, sent string) bool {
	return c.authGate.tripOnce(err, sent, c.currentAuth(ctx))
}

func sentAuth(ctx context.Context) string {
	md, _ := metadata.FromOutgoingContext(ctx)
	if v := md.Get("authorization"); len(v) > 0 {
		return v[0]
	}

	return ""
}

func (c *Client) getMethodCallMetadata(ctx context.Context, logMD bool) metadata.MD {
	auth := c.authSource.Get(ctx)
	md := metadata.Pairs(
		"authorization", bearer(auth.Token),
		"x-flare-buildtool", c.clientName)

	if c.cacheOperationID != "" {
		md.Set("x-cache-operation-id", c.cacheOperationID)
	}

	if auth.WorkspaceID != "" {
		md.Set("x-org-id", auth.WorkspaceID)
	}
	if c.cacheConfigMetadata.BitriseAppID != "" {
		md.Set("x-app-id", c.cacheConfigMetadata.BitriseAppID)
	}
	if c.cacheConfigMetadata.BitriseBuildID != "" {
		md.Set("x-flare-build-id", c.cacheConfigMetadata.BitriseBuildID)
	}
	if c.cacheConfigMetadata.BitriseWorkflowName != "" {
		md.Set("x-workflow-name", c.cacheConfigMetadata.BitriseWorkflowName)
	}
	if c.cacheConfigMetadata.BitriseStepExecutionID != "" {
		md.Set("x-flare-step-id", c.cacheConfigMetadata.BitriseStepExecutionID)
	}
	if c.cacheConfigMetadata.GitMetadata.RepoURL != "" {
		md.Set("x-repository-url", c.cacheConfigMetadata.GitMetadata.RepoURL)
	}
	if c.cacheConfigMetadata.CIProvider != "" {
		md.Set("x-ci-provider", c.cacheConfigMetadata.CIProvider)
	}

	md.Set("x-flare-blob-validation-level", "WARN")
	md.Set("x-flare-ac-validation-mode", "fast")

	rmd := &remoteexecution.RequestMetadata{
		ToolInvocationId: c.invocationID,
		ToolDetails: &remoteexecution.ToolDetails{
			ToolName: c.clientName,
		},
	}
	serializedRMD, err := proto.Marshal(rmd)
	if err != nil {
		c.logger.Errorf("Failed to marshal RequestMetadata: %v", err)
	} else {
		md.Set("build.bazel.remote.execution.v2.requestmetadata-bin", string(serializedRMD))
	}

	if logMD {
		logMd := md.Copy()
		logMd.Delete("authorization")
		logMd.Set("build.bazel.remote.execution.v2.requestmetadata-bin", rmd.String())
		c.logger.TDebugf("metadata: %+v", logMd)
	}

	return md
}

func (c *Client) QueryWriteStatus(ctx context.Context, name string) (WriteStatus, error) {
	if c.authBroken(ctx) {
		return WriteStatus{}, ErrCacheUnauthenticated
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	ch := c.pickChannel()
	if err := c.acquireOn(timeoutCtx, ch); err != nil {
		return WriteStatus{}, err
	}
	defer ch.release()

	resourceName := fmt.Sprintf("kv/%s", name)

	callCtx := metadata.NewOutgoingContext(timeoutCtx, c.getMethodCallMetadata(ctx, false))
	resp, err := ch.bitriseKVClient.WriteStatus(callCtx, &bytestream.QueryWriteStatusRequest{
		ResourceName: resourceName,
	})
	if err != nil {
		if c.tripAuth(ctx, err, sentAuth(callCtx)) {
			return WriteStatus{}, ErrCacheUnauthenticated
		}

		return WriteStatus{}, fmt.Errorf("query write status: %w", err)
	}

	return WriteStatus{
		Complete:      resp.GetComplete(),
		CommittedSize: resp.GetCommittedSize(),
	}, nil
}
