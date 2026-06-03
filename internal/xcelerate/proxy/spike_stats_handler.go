package proxy

// Spike instrumentation for ACI-5040 (Xcode.app ↔ proxy socket connection lifecycle).
// Gated by env var BITRISE_BUILD_CACHE_SPIKE_STATS=1. Remove with the rest of the
// spike branch once the question is answered.

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	grpcstats "google.golang.org/grpc/stats"
)

// spikeStatsHandlerEnabled reports whether the spike instrumentation should be wired in.
func spikeStatsHandlerEnabled() bool {
	return os.Getenv("BITRISE_BUILD_CACHE_SPIKE_STATS") == "1"
}

type (
	connCtxKey struct{}
	rpcCtxKey  struct{}
)

type connInfo struct {
	id        uint64
	startedAt time.Time
	rpcCount  atomic.Int64
}

type rpcInfo struct {
	method    string
	startedAt time.Time
}

type spikeStatsHandler struct {
	logger  log.Logger
	connSeq atomic.Uint64
}

func newSpikeStatsHandler(logger log.Logger) *spikeStatsHandler {
	return &spikeStatsHandler{logger: logger}
}

func (h *spikeStatsHandler) TagConn(ctx context.Context, info *grpcstats.ConnTagInfo) context.Context {
	id := h.connSeq.Add(1)
	c := &connInfo{id: id, startedAt: time.Now()}

	h.logger.Infof("[spike] TagConn id=%d remote=%s local=%s",
		id,
		safeAddr(info.RemoteAddr),
		safeAddr(info.LocalAddr),
	)

	return context.WithValue(ctx, connCtxKey{}, c)
}

func (h *spikeStatsHandler) HandleConn(ctx context.Context, s grpcstats.ConnStats) {
	c, ok := ctx.Value(connCtxKey{}).(*connInfo)
	if !ok {
		return
	}

	switch s.(type) {
	case *grpcstats.ConnBegin:
		h.logger.Infof("[spike] ConnBegin id=%d", c.id)
	case *grpcstats.ConnEnd:
		h.logger.Infof("[spike] ConnEnd id=%d duration=%s rpcs=%d",
			c.id,
			time.Since(c.startedAt),
			c.rpcCount.Load(),
		)
	}
}

func (h *spikeStatsHandler) TagRPC(ctx context.Context, info *grpcstats.RPCTagInfo) context.Context {
	r := &rpcInfo{method: info.FullMethodName, startedAt: time.Now()}

	if c, ok := ctx.Value(connCtxKey{}).(*connInfo); ok {
		c.rpcCount.Add(1)
		h.logger.Debugf("[spike] TagRPC conn=%d method=%s", c.id, r.method)
	} else {
		h.logger.Debugf("[spike] TagRPC method=%s (no conn ctx)", r.method)
	}

	return context.WithValue(ctx, rpcCtxKey{}, r)
}

func (h *spikeStatsHandler) HandleRPC(ctx context.Context, s grpcstats.RPCStats) {
	r, ok := ctx.Value(rpcCtxKey{}).(*rpcInfo)
	if !ok {
		return
	}

	switch v := s.(type) {
	case *grpcstats.Begin:
		c := connIDFrom(ctx)
		h.logger.Infof("[spike] RPCBegin conn=%s method=%s t=%d clientStream=%t serverStream=%t",
			c, r.method, time.Now().UnixNano(), v.IsClientStream, v.IsServerStream)
	case *grpcstats.End:
		c := connIDFrom(ctx)
		errStr := "ok"
		if v.Error != nil {
			errStr = v.Error.Error()
		}
		h.logger.Infof("[spike] RPCEnd conn=%s method=%s t=%d duration=%s err=%s",
			c, r.method, time.Now().UnixNano(), time.Since(r.startedAt), errStr)
	}
}

func connIDFrom(ctx context.Context) string {
	c, ok := ctx.Value(connCtxKey{}).(*connInfo)
	if !ok {
		return "?"
	}

	return fmt.Sprintf("%d", c.id)
}

func safeAddr(a interface{ String() string }) string {
	if a == nil {
		return "<nil>"
	}

	return a.String()
}
