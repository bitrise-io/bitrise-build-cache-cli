package kv

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc/metadata"
)

type PutParams struct {
	Name      string
	Sha256Sum string
	FileSize  int64
}

func (c *Client) Put(ctx context.Context, params PutParams) (io.WriteCloser, error) {
	md := metadata.Pairs(
		"authorization", fmt.Sprintf("bearer %s", c.authConfig.AuthToken),
		"x-flare-buildtool", "xcode",
		"x-flare-blob-validation-sha256", params.Sha256Sum,
		"x-flare-blob-validation-level", "error",
		"x-flare-no-skip-duplicate-writes", "true",
	)
	if c.authConfig.WorkspaceID != "" {
		md.Append("x-org-id", c.authConfig.WorkspaceID)
	}
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

	readReq := &bytestream.ReadRequest{
		ResourceName: resourceName,
		ReadOffset:   0,
		ReadLimit:    0,
	}
	md := metadata.Pairs(
		"authorization", fmt.Sprintf("bearer %s", c.authConfig.AuthToken),
		"x-flare-buildtool", "xcode")
	if c.authConfig.WorkspaceID != "" {
		md.Append("x-org-id", c.authConfig.WorkspaceID)
	}
	ctx = metadata.NewOutgoingContext(ctx, md)
	stream, err := c.bitriseKVClient.Get(ctx, readReq)
	if err != nil {
		return nil, fmt.Errorf("initiate get: %w", err)
	}

	return &reader{
		stream: stream,
		buf:    bytes.Buffer{},
	}, nil
}
