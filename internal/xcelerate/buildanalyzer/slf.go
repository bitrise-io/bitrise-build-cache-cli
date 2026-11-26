package buildanalyzer

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
)

// SLF (Structured Log Format) token kinds. Each token starts with ASCII
// digits encoding either the value (int) or the payload length / class pool
// index, followed by a single sentinel byte, optionally followed by a
// payload. See XCLogParser's LoggingTokenParser for the reference grammar.
type tokenKind int

const (
	tokenInt            tokenKind = iota // `N#` — integer value N
	tokenString                          // `N"<N bytes>` — string of length N
	tokenDouble                          // `<16 hex chars>^` — IEEE-754 LE double
	tokenNil                             // `-` — nil / null
	tokenClassName                       // `N%<N bytes>` — class name decl of length N
	tokenClassRef                        // `N@` — ref to class pool index N (1-indexed; 0 = nil)
	tokenListHeader                      // `N(` — tuple/list header, N elements follow
	tokenJSONAttachment                  // `N*<N bytes>` — JSON-encoded attachment payload
)

type token struct {
	kind     tokenKind
	intVal   int64
	strVal   string
	floatVal float64
}

// slfReader is a pull parser over a decompressed SLF body. It owns the full
// decompressed []byte (same reason as internal/xcactivitylog — SLF payloads
// carry multi-MB rows that trip bufio.Scanner even with a bumped buffer).
type slfReader struct {
	body []byte
	pos  int
}

const slfMagic = "SLF0"

func newSLFReader(body []byte) (*slfReader, error) {
	if len(body) < len(slfMagic) {
		return nil, errors.New("slf: body shorter than magic")
	}
	if string(body[:len(slfMagic)]) != slfMagic {
		return nil, fmt.Errorf("slf: bad magic: %q", body[:len(slfMagic)])
	}

	return &slfReader{body: body, pos: len(slfMagic)}, nil
}

func (r *slfReader) atEOF() bool { return r.pos >= len(r.body) }

// next advances past one token. Unknown sentinels are skipped as a no-op so
// a single Xcode-side format drift doesn't poison the whole scan.
func (r *slfReader) next() (token, error) {
	if r.atEOF() {
		return token{}, errEOF
	}

	// A token header is either ASCII digits ending in a sentinel, or a bare
	// `-` (nil), or 16 hex chars ending in `^` (double literal).
	start := r.pos
	for r.pos < len(r.body) {
		b := r.body[r.pos]
		switch {
		case b >= '0' && b <= '9':
			r.pos++
		case b == '-':
			r.pos++

			return token{kind: tokenNil}, nil
		case b >= 'a' && b <= 'f':
			// Hex prefix of a double — advance until `^`.
			return r.readDouble(start)
		default:
			return r.readSentinel(start, b)
		}
	}

	return token{}, errEOF
}

func (r *slfReader) readDouble(start int) (token, error) {
	for r.pos < len(r.body) {
		b := r.body[r.pos]
		if b == '^' {
			hexStr := string(r.body[start:r.pos])
			r.pos++ // consume `^`

			if len(hexStr) != 16 {
				return token{}, fmt.Errorf("slf: double literal wrong length at %d: %q", start, hexStr)
			}

			raw, err := hex.DecodeString(hexStr)
			if err != nil {
				return token{}, fmt.Errorf("slf: double hex decode at %d: %w", start, err)
			}

			bits := binary.LittleEndian.Uint64(raw)

			return token{kind: tokenDouble, floatVal: math.Float64frombits(bits)}, nil
		}
		if !isHex(b) {
			return token{}, fmt.Errorf("slf: bad double hex at %d: %q", start, b)
		}
		r.pos++
	}

	return token{}, fmt.Errorf("slf: unterminated double literal from %d", start)
}

func (r *slfReader) readSentinel(start int, sentinel byte) (token, error) {
	// Numeric header: digits from `start`..current; sentinel is the current byte.
	numStr := string(r.body[start:r.pos])
	var n int64
	if numStr != "" {
		// Xcode writes UInt64 sentinel flags like `18446744073709551615` (=
		// max uint64) for optional ints; parse unsigned then cast so those
		// don't surface as parse errors. Signed overflow wraps to a stable
		// sentinel — callers that care treat it as nil.
		u, err := strconv.ParseUint(numStr, 10, 64)
		if err != nil {
			return token{}, fmt.Errorf("slf: bad digits at %d: %w", start, err)
		}

		n = int64(u) //nolint:gosec // intentional wrap: ints are opaque flags at this layer
	}

	r.pos++ // consume sentinel

	switch sentinel {
	case '#':
		return token{kind: tokenInt, intVal: n}, nil
	case '@':
		return token{kind: tokenClassRef, intVal: n}, nil
	case '(':
		return token{kind: tokenListHeader, intVal: n}, nil
	case '"':
		return r.consumePayload(tokenString, n)
	case '%':
		return r.consumePayload(tokenClassName, n)
	case '*':
		return r.consumePayload(tokenJSONAttachment, n)
	case '^':
		// `^` without hex prefix shouldn't occur — treat as a stray nil to keep walking.
		return token{kind: tokenNil}, nil
	default:
		return token{}, fmt.Errorf("slf: unknown sentinel %q at %d (numeric %q)", sentinel, start, numStr)
	}
}

func (r *slfReader) consumePayload(kind tokenKind, n int64) (token, error) {
	if n < 0 || r.pos+int(n) > len(r.body) {
		return token{}, fmt.Errorf("slf: payload length %d out of bounds at %d", n, r.pos)
	}

	val := string(r.body[r.pos : r.pos+int(n)])
	r.pos += int(n)

	return token{kind: kind, intVal: n, strVal: val}, nil
}

var errEOF = errors.New("slf: EOF")

func isHex(b byte) bool {
	switch {
	case b >= '0' && b <= '9':
		return true
	case b >= 'a' && b <= 'f':
		return true
	default:
		return false
	}
}
