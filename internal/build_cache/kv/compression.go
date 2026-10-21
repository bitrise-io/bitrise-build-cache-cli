package kv

import (
	"bytes"
	"fmt"
	"io"
	"runtime"

	"github.com/klauspost/compress/zstd"
)

// compressedNamePrefix selects the zstd form; the backend converts between forms on read.
const compressedNamePrefix = "zstd/"

// DefaultCompressionThreshold matches Bazel's --experimental_remote_cache_compression_threshold.
const DefaultCompressionThreshold int64 = 100

// Xcode CAS blobs reach ~1 GiB; the cap only bounds a corrupt frame.
const maxDecodedBytes = 4 << 30

type CompressionParams struct {
	Enabled bool
	// Threshold is the smallest blob, in bytes, that is compressed on upload; <= 0 means DefaultCompressionThreshold.
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

func (c *compression) compress(source io.ReadSeeker, size int64) (*bytes.Reader, bool, error) {
	if size < c.threshold {
		return nil, false, nil
	}

	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return nil, false, fmt.Errorf("seek to start: %w", err)
	}

	buf := bytes.NewBuffer(make([]byte, 0, size))
	if _, err := io.Copy(buf, source); err != nil {
		return nil, false, fmt.Errorf("read blob: %w", err)
	}

	encoded := c.enc.EncodeAll(buf.Bytes(), make([]byte, 0, size/2))
	if int64(len(encoded)) >= size {
		return nil, false, nil
	}

	return bytes.NewReader(encoded), true, nil
}

func (c *compression) decompressTo(destination io.Writer, compressed []byte) error {
	raw, err := c.dec.DecodeAll(compressed, nil)
	if err != nil {
		return fmt.Errorf("zstd decode: %w", err)
	}

	if _, err := destination.Write(raw); err != nil {
		return fmt.Errorf("write decoded blob: %w", err)
	}

	return nil
}

func (c *compression) close() {
	_ = c.enc.Close()
	c.dec.Close()
}
