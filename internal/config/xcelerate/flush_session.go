package xcelerate

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/llvm/session"
)

const flushSessionRPCTimeout = 30 * time.Second

// FlushSessionClient captures the subset of the proxy session RPC the flush
// path depends on; `*grpc.ClientConn`-shaped callers wrap `session.NewSessionClient`.
type FlushSessionClient interface {
	FlushSession(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*session.FlushSessionResponse, error)
}

// FlushSession dials the running proxy, invokes the FlushSession RPC, and
// prints a Visit URL per emitted invocation ID. Missing proxy is a soft no-op.
func FlushSession(ctx context.Context, logger log.Logger, osProxy utils.OsProxy, stdout io.Writer) error {
	config, err := ReadConfig(osProxy, utils.DefaultDecoderFactory{}, utils.AllEnvs())
	if err != nil {
		return fmt.Errorf("read xcelerate config: %w", err)
	}

	if _, running := ProxyOwner(osProxy); !running {
		logger.TWarnf("No xcelerate-proxy is running; nothing to flush")

		return nil
	}

	clientConn, closeFn, err := dialProxy(config.ProxySocketPath)
	if err != nil {
		logger.Warnf("Failed to dial xcelerate-proxy: %s", err)

		return nil
	}
	defer closeFn()

	return printEmittedURLs(ctx, logger, stdout, session.NewSessionClient(clientConn))
}

// printEmittedURLs invokes FlushSession and prints the resulting Visit URLs.
// Separated so tests can inject a fake client without a real socket.
func printEmittedURLs(ctx context.Context, logger log.Logger, stdout io.Writer, client FlushSessionClient) error {
	callCtx, cancel := context.WithTimeout(ctx, flushSessionRPCTimeout)
	defer cancel()

	resp, err := client.FlushSession(callCtx, &emptypb.Empty{})
	if err != nil {
		logger.Warnf("FlushSession RPC failed: %s", err)

		return nil
	}

	for _, id := range resp.GetEmittedInvocationIds() {
		fmt.Fprintf(stdout, "Invocation saved. Visit 👉 %s\n", enrichment.VisitURL(id))
	}

	return nil
}

func dialProxy(socketPath string) (*grpc.ClientConn, func(), error) {
	target := "unix://" + strings.TrimPrefix(socketPath, "unix://")

	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, fmt.Errorf("grpc client: %w", err)
	}

	return conn, func() { _ = conn.Close() }, nil
}
