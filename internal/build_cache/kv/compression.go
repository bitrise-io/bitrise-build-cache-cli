package kv

import (
	"bytes"
	"fmt"
	"io"
	"runtime"

	"github.com/klauspost/compress/zstd"
)

// compressedNamePrefix selects the zstd wire form; the backend stores the blob uncompressed either way.
const compressedNamePrefix = "zstd/"

// DefaultCompressionThreshold matches Bazel's --experimental_remote_cache_compression_threshold.
const DefaultCompressionThreshold int64 = 100

// Xcode CAS blobs reach ~1 GiB; the cap only bounds a corrupt frame.
const maxDecodedBytes = 4 << 30

type CompressionParams struct {
	Enabled bool
	// Threshold is the smallest blob, in bytes, that is sent compressed; smaller ones go as is. <= 0 means DefaultCompressionThreshold.
	Threshold int64
}

type compression struct {
	threshold int64
	enc       *zstd.Encoder
	dec       *zstd.Decoder
}

func newCompression(p CompressionParams) (*compression, error) {
	if !p.Enabled {
		return nil, nil //nolint:nilnil
	}

	if p.Threshold <= 0 {
		p.Threshold = DefaultCompressionThreshold
	}

	// SpeedFastest: 5-7% less ratio than the default level for ~20-30% less CPU on Xcode blobs.
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		return nil, fmt.Errorf("new zstd encoder: %w", err)
	}

	// The default caps decoder concurrency at 4, which would throttle parallel proxy Gets.
	dec, err := zstd.NewReader(nil,
		zstd.WithDecoderMaxMemory(maxDecodedBytes),
		zstd.WithDecoderConcurrency(runtime.GOMAXPROCS(0)))
	if err != nil {
		return nil, fmt.Errorf("new zstd decoder: %w", err)
	}

	return &compression{threshold: p.Threshold, enc: enc, dec: dec}, nil
}

// compressedPayload is a blob's zstd form. Offsets refer to the uncompressed blob, as on the server.
type compressedPayload struct {
	enc  *zstd.Encoder
	raw  []byte
	full []byte
}

func (p *compressedPayload) from(offset int64) (io.Reader, int64, error) {
	if offset <= 0 {
		return bytes.NewReader(p.full), int64(len(p.full)), nil
	}

	if offset >= int64(len(p.raw)) {
		return nil, 0, fmt.Errorf("offset %d is past the blob (%d bytes)", offset, len(p.raw))
	}

	tail := p.enc.EncodeAll(p.raw[offset:], nil)

	return bytes.NewReader(tail), int64(len(tail)), nil
}

// compress returns the zstd form of source, or ok=false for blobs below the threshold or that do not shrink.
func (c *compression) compress(source io.ReadSeeker, size int64) (*compressedPayload, bool, error) {
	if size <= 0 || size < c.threshold {
		return nil, false, nil
	}

	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return nil, false, fmt.Errorf("seek to start: %w", err)
	}

	buf := bytes.NewBuffer(make([]byte, 0, size))
	if _, err := io.Copy(buf, source); err != nil {
		return nil, false, fmt.Errorf("read blob: %w", err)
	}

	full := c.enc.EncodeAll(buf.Bytes(), make([]byte, 0, size/2))
	if int64(len(full)) >= size {
		return nil, false, nil
	}

	return &compressedPayload{enc: c.enc, raw: buf.Bytes(), full: full}, true, nil
}

func (c *compression) decompress(compressed []byte) ([]byte, error) {
	raw, err := c.dec.DecodeAll(compressed, nil)
	if err != nil {
		return nil, fmt.Errorf("zstd decode: %w", err)
	}

	return raw, nil
}

func (c *compression) close() {
	_ = c.enc.Close()
	c.dec.Close()
}
