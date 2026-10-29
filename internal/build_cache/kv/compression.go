package kv

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
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

// compression keeps free lists of streaming coders, which are costly to build; a coder serves one transfer at a time.
type compression struct {
	threshold int64
	encoders  chan *zstd.Encoder
	decoders  chan *zstd.Decoder
}

func newCompression(p CompressionParams) *compression {
	if !p.Enabled {
		return nil
	}

	if p.Threshold <= 0 {
		p.Threshold = DefaultCompressionThreshold
	}

	poolSize := runtime.GOMAXPROCS(0)

	return &compression{
		threshold: p.Threshold,
		encoders:  make(chan *zstd.Encoder, poolSize),
		decoders:  make(chan *zstd.Decoder, poolSize),
	}
}

func (c *compression) getEncoder() (*zstd.Encoder, error) {
	select {
	case e := <-c.encoders:
		return e, nil
	default:
	}

	// SpeedFastest: 5-7% less ratio than the default level for ~20-30% less CPU on Xcode blobs.
	e, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithEncoderConcurrency(1))
	if err != nil {
		return nil, fmt.Errorf("new zstd encoder: %w", err)
	}

	return e, nil
}

func (c *compression) putEncoder(e *zstd.Encoder) {
	e.Reset(nil)

	select {
	case c.encoders <- e:
	default:
		_ = e.Close()
	}
}

func (c *compression) getDecoder() (*zstd.Decoder, error) {
	select {
	case d := <-c.decoders:
		return d, nil
	default:
	}

	d, err := zstd.NewReader(nil, zstd.WithDecoderMaxMemory(maxDecodedBytes), zstd.WithDecoderConcurrency(1))
	if err != nil {
		return nil, fmt.Errorf("new zstd decoder: %w", err)
	}

	return d, nil
}

func (c *compression) putDecoder(d *zstd.Decoder) {
	_ = d.Reset(nil)

	select {
	case c.decoders <- d:
	default:
		d.Close()
	}
}

// encode streams src through a zstd encoder into memory, so only the compressed form is held, never a raw copy.
// It returns the encoded bytes and how many raw bytes were read.
func (c *compression) encode(src io.Reader, sizeHint int64) ([]byte, int64, error) {
	enc, err := c.getEncoder()
	if err != nil {
		return nil, 0, err
	}

	var out bytes.Buffer
	if grow := sizeHint / 2; grow > 0 && grow <= math.MaxInt32 {
		out.Grow(int(grow))
	}

	enc.Reset(&out)

	n, err := io.Copy(enc, src)
	if err == nil {
		err = enc.Close()
	}

	if err != nil {
		_ = enc.Close()

		return nil, n, fmt.Errorf("zstd encode: %w", err)
	}

	c.putEncoder(enc)

	return out.Bytes(), n, nil
}

// decodeTo streams the decoded blob into dst without materialising it, and returns the decoded byte count.
func (c *compression) decodeTo(dst io.Writer, compressed []byte) (int64, error) {
	dec, err := c.getDecoder()
	if err != nil {
		return 0, err
	}

	if err := dec.Reset(bytes.NewReader(compressed)); err != nil {
		dec.Close()

		return 0, fmt.Errorf("zstd decode: %w", err)
	}

	n, err := io.Copy(dst, io.LimitReader(dec, int64(maxDecodedBytes)+1))
	if err == nil && n > int64(maxDecodedBytes) {
		err = errors.New("decoded blob exceeds the size limit")
	}

	if err != nil {
		dec.Close()

		return n, fmt.Errorf("zstd decode: %w", err)
	}

	c.putDecoder(dec)

	return n, nil
}

// twoPassDecodeMin is the compressed size from which a download is decoded twice (hash and count, then write) so the
// destination is sized exactly; a destination that grows while it fills wastes more memory than that costs in CPU.
const twoPassDecodeMin = 1 << 20

// decodeInto writes the decoded blob to dst and calls verify with its size and sha256 (hex). A rejected large blob
// never reaches dst; a small one is written while it is hashed, so dst may hold it.
func (c *compression) decodeInto(dst io.Writer, compressed []byte, verify func(decoded int64, sumHex string) error) (int64, error) {
	h := sha256.New()

	if len(compressed) < twoPassDecodeMin {
		n, err := c.decodeTo(io.MultiWriter(dst, h), compressed)
		if err != nil {
			return n, err
		}

		return n, verify(n, hex.EncodeToString(h.Sum(nil)))
	}

	n, err := c.decodeTo(h, compressed)
	if err != nil {
		return n, err
	}

	if err := verify(n, hex.EncodeToString(h.Sum(nil))); err != nil {
		return n, err
	}

	if g, ok := dst.(interface{ Grow(n int) }); ok && n <= math.MaxInt32 {
		g.Grow(int(n))
	}

	written, err := c.decodeTo(dst, compressed)
	if err == nil && written != n {
		err = fmt.Errorf("zstd decode: second pass produced %d bytes, first pass %d", written, n)
	}

	return written, err
}

// compressedPayload is a blob's zstd form. Offsets refer to the uncompressed blob, as on the server.
type compressedPayload struct {
	c      *compression
	source io.ReadSeeker
	size   int64
	full   []byte
}

// from re-reads and re-encodes the tail on a resumed attempt, so the raw blob is not held for the whole upload.
func (p *compressedPayload) from(offset int64) (io.Reader, int64, error) {
	if offset <= 0 {
		return bytes.NewReader(p.full), int64(len(p.full)), nil
	}

	if offset >= p.size {
		return nil, 0, fmt.Errorf("offset %d is past the blob (%d bytes)", offset, p.size)
	}

	if _, err := p.source.Seek(offset, io.SeekStart); err != nil {
		return nil, 0, fmt.Errorf("seek source to %d: %w", offset, err)
	}

	want := p.size - offset

	tail, n, err := p.c.encode(io.LimitReader(p.source, want), want)
	if err != nil {
		return nil, 0, err
	}

	if n != want {
		return nil, 0, fmt.Errorf("read blob tail: got %d of %d bytes", n, want)
	}

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

	full, n, err := c.encode(io.LimitReader(source, size), size)
	if err != nil {
		return nil, false, err
	}

	if n != size {
		return nil, false, fmt.Errorf("read blob: got %d of %d bytes", n, size)
	}

	if int64(len(full)) >= size {
		return nil, false, nil
	}

	return &compressedPayload{c: c, source: source, size: size, full: full}, true, nil
}

func (c *compression) close() {
	for {
		select {
		case e := <-c.encoders:
			_ = e.Close()
		case d := <-c.decoders:
			d.Close()
		default:
			return
		}
	}
}
