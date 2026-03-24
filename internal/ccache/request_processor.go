package ccache

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache/protocol"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
)

type requestProcessor struct {
	client          Client
	logger          log.Logger
	reader          io.Reader
	writer          io.Writer
	ccSemaphore     chan struct{}
	config          ccacheconfig.Config
	metadata        configcommon.CacheConfigMetadata
	loggerFactory   LoggerFactory
	getCapabilities func(context.Context) error
}

func newRequestProcessor(
	conn io.ReadWriter,
	config ccacheconfig.Config,
	metadata configcommon.CacheConfigMetadata,
	client Client,
	logger log.Logger,
	loggerFactory LoggerFactory,
	getCapabilities func(context.Context) error,
) *requestProcessor {
	sem := make(chan struct{}, 1)
	sem <- struct{}{} // pre-fill: receiving acquires, sending releases

	return &requestProcessor{
		config:          config,
		metadata:        metadata,
		client:          client,
		logger:          logger,
		reader:          conn,
		writer:          conn,
		ccSemaphore:     sem,
		loggerFactory:   loggerFactory,
		getCapabilities: getCapabilities,
	}
}

func (p *requestProcessor) notifyClient(result processResult) processResult {
	var err error

	p.logger.TDebugf("%s - sending to ccache", result.Log())

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
	return hex.EncodeToString(key)
}

func (p *requestProcessor) logCallStats(result processResult) {
	p.logger.TDebugf("%s took %s", result.Log(), time.Since(result.CallStats.start))
}

func (p *requestProcessor) initCapabilities(ctx context.Context) error {
	if err := p.getCapabilities(ctx); err != nil {
		return fmt.Errorf("failed to get capabilities: %w", err)
	}

	return nil
}

func (p *requestProcessor) handleGet(ctx context.Context) processResult {
	statBuilder := newStatBuilder(CALL_METHOD_GET)

	keyBytes, err := protocol.ReadKey(p.reader)
	if err != nil {
		return processResult{
			Outcome:   PROCESS_REQUEST_ERROR,
			Err:       fmt.Errorf("failed to read key: %w", err),
			CallStats: statBuilder.build(),
		}
	}

	key := p.keyToPath(keyBytes)
	statBuilder.withKey(key)
	p.logger.TDebugf("%s Called", statBuilder.Prefix())

	buffer := bytes.NewBuffer(nil)
	err = p.client.DownloadStream(ctx, buffer, key)

	switch {
	case err == nil:
		// success
	case errors.Is(err, kv.ErrCacheNotFound):
		return p.notifyClient(processResult{
			Outcome:   PROCESS_REQUEST_MISS,
			CallStats: statBuilder.build(),
		})
	default:
		return p.notifyClient(processResult{
			Outcome:   PROCESS_REQUEST_ERROR,
			Err:       fmt.Errorf("failed to download data: %w", err),
			CallStats: statBuilder.build(),
		})
	}

	size := int64(buffer.Len())
	statBuilder.withDownloadBytes(size)

	data, err := io.ReadAll(buffer)
	if err != nil {
		return p.notifyClient(processResult{
			Outcome:   PROCESS_REQUEST_ERROR,
			Err:       fmt.Errorf("failed to read data: %w", err),
			CallStats: statBuilder.build(),
		})
	}

	return p.notifyClient(processResult{
		Outcome:   PROCESS_REQUEST_OK,
		CallStats: statBuilder.build(),
		Data:      data,
	})
}

func (p *requestProcessor) handlePut(ctx context.Context) processResult {
	statBuilder := newStatBuilder(CALL_METHOD_PUT)

	keyBytes, err := protocol.ReadKey(p.reader)
	if err != nil {
		return processResult{
			Outcome:   PROCESS_REQUEST_ERROR,
			Err:       fmt.Errorf("failed to read key: %w", err),
			CallStats: statBuilder.build(),
		}
	}
	key := p.keyToPath(keyBytes)
	statBuilder.withKey(key)

	_, err = protocol.ReadByte(p.reader)
	if err != nil {
		return processResult{
			Outcome:   PROCESS_REQUEST_ERROR,
			Err:       fmt.Errorf("failed to read flags: %w", err),
			CallStats: statBuilder.build(),
		}
	}

	value, err := protocol.ReadValue(p.reader)
	if err != nil {
		return processResult{
			Outcome:   PROCESS_REQUEST_ERROR,
			Err:       fmt.Errorf("failed to read value: %w", err),
			CallStats: statBuilder.build(),
		}
	}

	if !p.config.PushEnabled {
		return p.notifyClient(processResult{
			Outcome:   PROCESS_REQUEST_PUSH_DISABLED,
			CallStats: statBuilder.build(),
		})
	}

	size := int64(len(value))
	statBuilder.withUploadBytes(size)
	p.logger.TDebugf("%s Called (%d bytes)", statBuilder.Prefix(), size)

	if err = p.client.UploadStreamToBuildCache(ctx, bytes.NewReader(value), key, size); err != nil {
		return p.notifyClient(processResult{
			Outcome:   PROCESS_REQUEST_ERROR,
			Err:       fmt.Errorf("failed to upload data: %w", err),
			CallStats: statBuilder.build(),
		})
	}

	return p.notifyClient(processResult{
		Outcome:   PROCESS_REQUEST_OK,
		CallStats: statBuilder.build(),
	})
}

func (p *requestProcessor) handleRemove() processResult {
	statBuilder := newStatBuilder(CALL_METHOD_REMOVE)
	keyBytes, err := protocol.ReadKey(p.reader)
	if err != nil {
		return processResult{
			Outcome: PROCESS_REQUEST_ERROR,
			Err:     err,
		}
	}

	key := p.keyToPath(keyBytes)
	statBuilder.withKey(key)
	p.logger.TDebugf("%s Called", statBuilder.Prefix())

	// We handle removal on the storage helper level by keeping track of keys.
	// See ipc_server.go
	return p.notifyClient(processResult{
		Outcome:   PROCESS_REQUEST_OK,
		CallStats: statBuilder.build(),
	})
}

func (p *requestProcessor) handleSetInvocationID() processResult {
	statBuilder := newStatBuilder(CALL_METHOD_SET_INVOCATION_ID)

	parentID, childID, err := protocol.ReadSetInvocationID(p.reader)
	if err != nil {
		return processResult{
			Outcome:   PROCESS_REQUEST_ERROR,
			Err:       fmt.Errorf("failed to read invocation ID: %w", err),
			CallStats: statBuilder.build(),
		}
	}

	p.logger.TDebugf("[SetInvocationID] parent=%s child=%s", parentID, childID)

	if p.loggerFactory != nil {
		newLogger, logErr := p.loggerFactory(childID)
		if logErr != nil {
			p.logger.TErrorf("[SetInvocationID] Failed to create logger for invocation %s: %v", childID, logErr)
		} else {
			p.logger = newLogger
		}
	}

	p.client.ChangeSession(childID, p.metadata.BitriseAppID, p.metadata.BitriseBuildID, p.metadata.BitriseStepExecutionID)

	return p.notifyClient(processResult{
		Outcome:            PROCESS_REQUEST_OK,
		CallStats:          statBuilder.build(),
		InvocationParentID: parentID,
		InvocationChildID:  childID,
	})
}

func (p *requestProcessor) handleStop() processResult {
	statBuilder := newStatBuilder(CALL_METHOD_STOP)
	p.logger.TDebugf("%s received, requesting shutdown", statBuilder.Prefix())

	// Response is written by handleConnection after the shutdown callback completes.
	return processResult{
		Outcome:   PROCESS_REQUEST_OK,
		CallStats: statBuilder.build(),
	}
}

func (p *requestProcessor) processRequest(ctx context.Context) processResult {
	reqType, err := protocol.ReadRequest(p.reader)
	if err != nil {
		return processResult{
			Outcome: PROCESS_REQUEST_ERROR,
			Err:     err,
		}
	}

	select {
	case <-ctx.Done():
		return processResult{
			Outcome: PROCESS_REQUEST_ERROR,
			Err:     fmt.Errorf("context cancelled while waiting for semaphore: %w", ctx.Err()),
		}
	case <-p.ccSemaphore:
	}
	defer func() { p.ccSemaphore <- struct{}{} }()

	var result processResult
	defer func() { p.logCallStats(result) }()

	switch reqType {
	case protocol.RequestGet:
		result = p.handleGet(ctx)

		return result

	case protocol.RequestPut:
		result = p.handlePut(ctx)

		return result

	case protocol.RequestRemove:
		result = p.handleRemove()

		return result

	case protocol.RequestStop:
		result = p.handleStop()

		return result

	case protocol.RequestSetInvocationID:
		result = p.handleSetInvocationID()

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
