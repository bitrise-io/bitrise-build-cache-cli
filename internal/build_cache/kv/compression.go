package kv

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"runtime"

	"github.com/klauspost/compress/zstd"
)

// compressedNamePrefix selects the zstd form; every upload and download of a compressing client uses it.
const compressedNamePrefix = "zstd/"

// DefaultCompressionThreshold matches Bazel's --experimental_remote_cache_compression_threshold.
const DefaultCompressionThreshold int64 = 100

// Xcode CAS blobs reach ~1 GiB; the cap only bounds a corrupt frame.
const maxDecodedBytes = 4 << 30

type CompressionParams struct {
	Enabled bool
	// Threshold is the smallest blob, in bytes, that is compressed; smaller ones are framed as is. <= 0 means DefaultCompressionThreshold.
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

// compress returns the blob as a zstd frame; below the threshold the frame carries the data uncompressed.
func (c *compression) compress(source io.ReadSeeker, size int64) (*bytes.Reader, error) {
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek to start: %w", err)
	}

	buf := bytes.NewBuffer(make([]byte, 0, size))
	if _, err := io.Copy(buf, source); err != nil {
		return nil, fmt.Errorf("read blob: %w", err)
	}

	if size < c.threshold {
		return bytes.NewReader(rawFrame(buf.Bytes())), nil
	}

	return bytes.NewReader(c.enc.EncodeAll(buf.Bytes(), make([]byte, 0, size/2))), nil
}

const (
	zstdMagic         = 0xFD2FB528
	maxRawBlockSize   = 128 << 10
	rawFrameWindowLog = 0x38 // window descriptor: 128 KiB, the largest block a frame may carry
)

// rawFrame wraps data in a valid zstd frame of raw blocks (RFC 8878), without running the encoder.
func rawFrame(data []byte) []byte {
	frame := binary.LittleEndian.AppendUint32(make([]byte, 0, len(data)+16), zstdMagic)
	frame = append(frame, 0x00, rawFrameWindowLog) // no content size, no checksum, no dictionary

	for {
		chunk := data[:min(len(data), maxRawBlockSize)]
		data = data[len(chunk):]

		header := uint32(len(chunk)) << 3 //nolint:gosec // bounded by maxRawBlockSize
		if len(data) == 0 {
			header |= 1 // last block; block type 0 is raw
		}
		frame = append(frame, byte(header), byte(header>>8), byte(header>>16))
		frame = append(frame, chunk...)

		if len(data) == 0 {
			return frame
		}
	}
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
