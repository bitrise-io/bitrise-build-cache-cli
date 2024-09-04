package kv

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	remoteexecution "github.com/bitrise-io/bitrise-build-cache-cli/proto/build/bazel/remote/execution/v2"
	"github.com/dustin/go-humanize"
	"google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc/metadata"
)

type PutParams struct {
	Name      string
	Sha256Sum string
	FileSize  int64
}

type FileDigest struct {
	Sha256Sum   string
	SizeInBytes int64
}

func (c *Client) GetCapabilities(ctx context.Context) error {
	ctx = metadata.NewOutgoingContext(ctx, c.getMethodCallMetadata())

	_, err := c.capabilitiesClient.GetCapabilities(ctx, &remoteexecution.GetCapabilitiesRequest{})
	if err != nil {
		return fmt.Errorf("get capabilities: %w", err)
	}

	return nil
}

func (c *Client) Put(ctx context.Context, params PutParams) (io.WriteCloser, error) {
	md := metadata.Join(c.getMethodCallMetadata(), metadata.Pairs(
		"x-flare-blob-validation-sha256", params.Sha256Sum,
		"x-flare-blob-validation-level", "error",
		"x-flare-no-skip-duplicate-writes", "true",
	))
	ctx = metadata.NewOutgoingContext(ctx, md)

	stream, err := c.bitriseKVClient.Put(ctx)
	if err != nil {
		return nil, fmt.Errorf("initiate put: %w", err)
	}

	resourceName := fmt.Sprintf("%s/%s", c.clientName, params.Name)

	return &writer{
		stream:       stream,
		resourceName: resourceName,
		offset:       0,
		fileSize:     params.FileSize,
	}, nil
}

func (c *Client) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	resourceName := fmt.Sprintf("%s/%s", c.clientName, name)

	ctx = metadata.NewOutgoingContext(ctx, c.getMethodCallMetadata())

	readReq := &bytestream.ReadRequest{
		ResourceName: resourceName,
		ReadOffset:   0,
		ReadLimit:    0,
	}
	stream, err := c.bitriseKVClient.Get(ctx, readReq)
	if err != nil {
		return nil, fmt.Errorf("initiate get: %w", err)
	}

	return &reader{
		stream: stream,
		buf:    bytes.Buffer{},
	}, nil
}

func (c *Client) Delete(ctx context.Context, name string) error {
	resourceName := fmt.Sprintf("%s/%s", c.clientName, name)

	ctx = metadata.NewOutgoingContext(ctx, c.getMethodCallMetadata())

	readReq := &bytestream.ReadRequest{
		ResourceName: resourceName,
		ReadOffset:   0,
		ReadLimit:    0,
	}
	_, err := c.bitriseKVClient.Delete(ctx, readReq)
	if err != nil {
		return fmt.Errorf("initiate delete: %w", err)
	}

	return nil
}

func (c *Client) FindMissing(ctx context.Context, digests []*FileDigest) ([]*FileDigest, error) {
	ctx = metadata.NewOutgoingContext(ctx, c.getMethodCallMetadata())

	var missingBlobs []*FileDigest
	blobDigests := convertToBlobDigests(digests)
	req := &remoteexecution.FindMissingBlobsRequest{
		BlobDigests: blobDigests,
	}
	c.logger.Debugf("Size of FindMissingBlobs request for %d blobs is %s", len(digests), humanize.Bytes(uint64(len(req.String()))))
	gRPCLimitBytes := 4 * 1024 * 1024 // gRPC limit is 4 MiB
	if len(req.String()) > gRPCLimitBytes {
		// Chunk up request blobs to fit into gRPC limits
		// Calculate the unit size of a blob (in practice can differ to the theoretical sha256(32 bytes) + size(8 bytes) = 40 bytes)
		digestUnitSize := float64(len(req.String())) / float64(len(digests))
		maxDigests := int(float64(gRPCLimitBytes) / digestUnitSize)
		for startIndex := 0; startIndex < len(digests); startIndex += maxDigests {
			endIndex := startIndex + maxDigests
			if endIndex > len(digests) {
				endIndex = len(digests)
			}
			req.BlobDigests = blobDigests[startIndex:endIndex]

			c.logger.Debugf("Calling FindMissingBlobs for chunk: digests[%d:%d]", startIndex, endIndex)
			resp, err := c.casClient.FindMissingBlobs(ctx, req)

			if err != nil {
				return nil, fmt.Errorf("find missing blobs[%d:%d]: %w", startIndex, endIndex, err)
			}
			missingBlobs = append(missingBlobs, convertToFileDigests(resp.GetMissingBlobDigests())...)
		}
	} else {
		resp, err := c.casClient.FindMissingBlobs(ctx, req)

		if err != nil {
			return nil, fmt.Errorf("find missing blobs: %w", err)
		}
		missingBlobs = convertToFileDigests(resp.GetMissingBlobDigests())
	}

	return missingBlobs, nil
}

func convertToBlobDigests(digests []*FileDigest) []*remoteexecution.Digest {
	out := make([]*remoteexecution.Digest, 0, len(digests))

	for _, d := range digests {
		out = append(out, &remoteexecution.Digest{
			Hash:      d.Sha256Sum + "/0",
			SizeBytes: d.SizeInBytes,
		})
	}

	return out
}

func convertToFileDigests(digests []*remoteexecution.Digest) []*FileDigest {
	out := make([]*FileDigest, 0, len(digests))

	for _, d := range digests {
		out = append(out, &FileDigest{
			Sha256Sum:   strings.TrimSuffix(d.GetHash(), "/0"),
			SizeInBytes: d.GetSizeBytes(),
		})
	}

	return out
}

func (c *Client) getMethodCallMetadata() metadata.MD {
	md := metadata.Pairs(
		"authorization", fmt.Sprintf("bearer %s", c.authConfig.AuthToken),
		"x-flare-buildtool", "xcode")

	if c.authConfig.WorkspaceID != "" {
		md.Set("x-org-id", c.authConfig.WorkspaceID)
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
	if c.cacheConfigMetadata.RepoURL != "" {
		md.Set("x-repository-url", c.cacheConfigMetadata.RepoURL)
	}
	if c.cacheConfigMetadata.CIProvider != "" {
		md.Set("x-ci-provider", c.cacheConfigMetadata.CIProvider)
	}

	return md
}
