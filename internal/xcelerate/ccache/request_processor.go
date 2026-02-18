package ccache

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	cfg "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcelerate/ccache/protocol"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcelerate/proxy"
	"github.com/bitrise-io/go-utils/v2/log"
)

type requestProcessor struct {
	ctx         context.Context
	client      proxy.Client
	logger      log.Logger
	reader      io.Reader
	writer      io.Writer
	ccSemaphore chan struct{}
	config      cfg.Config
}

func newRequestProcessor(
	conn io.ReadWriter,
	config cfg.Config,
	client proxy.Client,
	logger log.Logger,
) *requestProcessor {
	// numChan := max(2, runtime.NumCPU()/6)
	// ccLimit := numChan * runtime.NumCPU()
	// logger.Infof("Setting up proxy with concurrency limit: %d", ccLimit)

	return &requestProcessor{
		ctx:         context.Background(),
		config:      config,
		client:      client,
		logger:      logger,
		reader:      conn,
		writer:      conn,
		ccSemaphore: make(chan struct{}, 1),
	}
}

type callStats struct {
	start         time.Time
	method        string
	key           string
	uploadBytes   int64
	downloadBytes int64
}

type statBuilder struct {
	stats callStats
}

func (b *statBuilder) with(f func(*callStats)) *statBuilder {
	f(&b.stats)
	return b
}

func (b *statBuilder) build() callStats {
	return b.stats
}

type processResultOutcome int32

const (
	PROCESS_REQUEST_OK            processResultOutcome = 0
	PROCESS_REQUEST_MISS          processResultOutcome = 1
	PROCESS_REQUEST_SHOULD_STOP   processResultOutcome = 2
	PROCESS_REQUEST_ERROR         processResultOutcome = 3
	PROCESS_REQUEST_PUSH_DISABLED processResultOutcome = 4
)

type processResult struct {
	Err       error
	Data      []byte
	CallStats callStats
	Outcome   processResultOutcome
}

func (result processResult) OutcomeString() string {
	switch result.Outcome {
	case PROCESS_REQUEST_OK:
		return "OK"
	case PROCESS_REQUEST_MISS:
		return "MISS"
	case PROCESS_REQUEST_SHOULD_STOP:
		return "SHOULD_STOP"
	case PROCESS_REQUEST_ERROR:
		return fmt.Sprintf("ERROR: %v", result.Err)
	case PROCESS_REQUEST_PUSH_DISABLED:
		return "PUSH_DISABLED"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", result.Outcome)
	}
}

func (result processResult) Prefix() string {
	return fmt.Sprintf("[%s - %s]", result.CallStats.method, result.CallStats.key)
}
func (result processResult) Log() string {
	return fmt.Sprintf("%s %s", result.Prefix(), result.OutcomeString())
}

func (p *requestProcessor) notifyCcache(result processResult) processResult {
	var err error

	p.logger.TDebugf(result.Log())

	switch result.Outcome {
	case PROCESS_REQUEST_ERROR:
		err = protocol.WriteErr(p.writer, result.Err.Error())

	case PROCESS_REQUEST_MISS, PROCESS_REQUEST_PUSH_DISABLED:
		err = protocol.WriteNoop(p.writer)

	case PROCESS_REQUEST_OK:
		err = p.writeOK(result)
	}

	if err != nil {
		return processResult{
			Outcome: PROCESS_REQUEST_ERROR,
			Err:     fmt.Errorf("failed to write response: %w", err),
		}
	}

	return result
}

func (p *requestProcessor) writeOK(result processResult) error {
	if err := protocol.WriteOK(p.writer); err != nil {
		return fmt.Errorf("%s Failed to write OK: %w", result.Prefix(), err)
	}

	if result.CallStats.method != "Get" {
		p.logger.TDebugf("%s Successfully wrote OK response", result.Prefix())
		return nil
	}

	if err := protocol.WriteValue(p.writer, result.Data); err != nil {
		return fmt.Errorf("%s Failed to write value: %w", result.Prefix(), err)
	}

	p.logger.TDebugf("%s Successfully wrote OK response with %d bytes of data", result.Prefix(), len(result.Data))

	return nil
}

func (p *requestProcessor) keyToPath(key []byte) string {
	keyHex := hex.EncodeToString(key)

	switch p.config.CCacheConfig.Layout {
	case "flat":
		return keyHex

	case "bazel":
		// Bazel format: ac/ + 64 hex digits, so pad shorter keys by repeating the key prefix to reach the expected SHA256 size.
		const sha256HexSize = 64
		if len(keyHex) >= sha256HexSize {
			return fmt.Sprintf("ac/%s", keyHex[:sha256HexSize])
		}
		return fmt.Sprintf("ac/%s%s", keyHex, keyHex[:sha256HexSize-len(keyHex)])

	default: // subdirs
		if len(keyHex) < 2 {
			return keyHex
		}
		return fmt.Sprintf("test-%s/%s", keyHex[:2], keyHex[2:])
	}
}

func (p *requestProcessor) logCallStats(result processResult) {
	p.logger.TDebugf("%s took %s, result: %s", result.Log(), time.Since(result.CallStats.start), result.OutcomeString())
}

func (p *requestProcessor) handleGet() processResult {
	statBuilder := &statBuilder{
		stats: callStats{
			method: "Get",
			start:  time.Now(),
		},
	}

	keyBytes, err := protocol.ReadKey(p.reader)
	if err != nil {
		return processResult{
			Outcome:   PROCESS_REQUEST_ERROR,
			Err:       fmt.Errorf("failed to read key: %w", err),
			CallStats: statBuilder.build(),
		}
	}

	key := p.keyToPath(keyBytes)
	statBuilder.with(func(s *callStats) { s.key = key })
	p.logger.TDebugf("[%s - %s] Called", statBuilder.stats.method, key)
	buffer := bytes.NewBuffer(nil)
	err = p.client.DownloadStream(p.ctx, buffer, key)

	switch {
	case err == nil:
		// success
	case errors.Is(err, kv.ErrCacheNotFound):
		p.logger.TDebugf("[%s - %s] Not found", statBuilder.stats.method, key)
		return p.notifyCcache(processResult{
			Outcome:   PROCESS_REQUEST_MISS,
			CallStats: statBuilder.build(),
		})
	default:
		return p.notifyCcache(processResult{
			Outcome:   PROCESS_REQUEST_ERROR,
			Err:       fmt.Errorf("failed to download data: %w", err),
			CallStats: statBuilder.build(),
		})
	}

	size := int64(buffer.Len())
	statBuilder.with(func(s *callStats) { s.downloadBytes = size })

	data, err := io.ReadAll(buffer)
	if err != nil {
		return p.notifyCcache(processResult{
			Outcome:   PROCESS_REQUEST_ERROR,
			Err:       fmt.Errorf("failed to read data: %w", err),
			CallStats: statBuilder.build(),
		})
	}

	return p.notifyCcache(processResult{
		Outcome:   PROCESS_REQUEST_OK,
		CallStats: statBuilder.build(),
		Data:      data,
	})
}

func (p *requestProcessor) handlePut() processResult {
	statBuilder := &statBuilder{
		stats: callStats{
			method: "Put",
			start:  time.Now(),
		},
	}

	keyBytes, err := protocol.ReadKey(p.reader)
	if err != nil {
		return processResult{
			Outcome:   PROCESS_REQUEST_ERROR,
			Err:       fmt.Errorf("failed to read key: %w", err),
			CallStats: statBuilder.build(),
		}
	}
	key := p.keyToPath(keyBytes)
	statBuilder.with(func(s *callStats) { s.key = key })

	_, err = protocol.ReadByte(p.reader)
	if err != nil {
		return processResult{
			Outcome:   PROCESS_REQUEST_ERROR,
			Err:       fmt.Errorf("failed to read flags: %w", err),
			CallStats: statBuilder.build(),
		}
	}
	// overwrite := (flags & protocol.PutFlagOverwrite) != 0
	// ignoring it at the moment

	value, err := protocol.ReadValue(p.reader)
	if err != nil {
		return processResult{
			Outcome:   PROCESS_REQUEST_ERROR,
			Err:       fmt.Errorf("failed to read value: %w", err),
			CallStats: statBuilder.build(),
		}
	}

	if !p.config.PushEnabled {
		p.logger.TDebugf("Save: Push disabled")

		return p.notifyCcache(processResult{
			Outcome:   PROCESS_REQUEST_PUSH_DISABLED,
			CallStats: statBuilder.build(),
		})
	}

	size := int64(len(value))
	statBuilder.with(func(s *callStats) { s.uploadBytes = size })
	p.logger.TDebugf("[%s - %s] Called (%d bytes)", statBuilder.stats.method, key, size)

	if err = p.client.UploadStreamToBuildCache(p.ctx, bytes.NewReader(value), key, size); err != nil {
		return p.notifyCcache(processResult{
			Outcome:   PROCESS_REQUEST_ERROR,
			Err:       fmt.Errorf("failed to upload data: %w", err),
			CallStats: statBuilder.build(),
		})
	}

	return p.notifyCcache(processResult{
		Outcome:   PROCESS_REQUEST_OK,
		CallStats: statBuilder.build(),
	})
}

func (p *requestProcessor) handleRemove() processResult {
	keyBytes, err := protocol.ReadKey(p.reader)
	if err != nil {
		return processResult{
			Outcome: PROCESS_REQUEST_ERROR,
			Err:     err,
		}
	}

	key := p.keyToPath(keyBytes)
	p.logger.TDebugf("[%s - Remove] Called", key)

	// We handle removal on the storage helper level by keeping track of keys.
	// See ipc_server.go
	return p.notifyCcache(processResult{
		Outcome: PROCESS_REQUEST_OK,
	})
}

func (p *requestProcessor) handleStop() processResult {
	p.logger.TDebugf("[Stop] Called")
	return processResult{
		Outcome: PROCESS_REQUEST_SHOULD_STOP,
	}
}

func (p *requestProcessor) processRequest() processResult {
	reqType, err := protocol.ReadRequest(p.reader)
	if err != nil {
		return processResult{
			Outcome: PROCESS_REQUEST_ERROR,
			Err:     err,
		}
	}

	p.ccSemaphore <- struct{}{}
	defer func() { <-p.ccSemaphore }()

	var result processResult
	defer func() { p.logCallStats(result) }()

	switch reqType {
	case protocol.RequestGet:
		result = p.handleGet()
		return result

	case protocol.RequestPut:
		result = p.handlePut()
		return result

	case protocol.RequestRemove:
		result = p.handleRemove()
		return result

	case protocol.RequestStop:
		result = p.handleStop()
		return result

	default:
		p.logger.TDebugf("Unknown request type: 0x%02x", reqType)
		result = processResult{
			Outcome: PROCESS_REQUEST_ERROR,
			Err:     fmt.Errorf("unknown request type: 0x%02x", reqType),
		}
		return result
	}
}
