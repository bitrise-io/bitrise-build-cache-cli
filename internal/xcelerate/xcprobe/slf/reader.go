package slf

import (
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
)

// ErrInvalidHeader is returned when the input is not an SLF stream.
var ErrInvalidHeader = errors.New("slf: missing SLF header")

// Lexer tokenises a decompressed SLF byte tape into a slice of Tokens.
// It is single-use and not safe for concurrent calls.
type Lexer struct {
	buf    []byte
	offset int

	// classNames holds the ordered list of class names seen (one entry per `%`
	// token). ClassNameRef (`@`) tokens index into this slice.
	classNames []string
}

// Version returns the SLF version integer sitting at the start of the stream
// (after the `SLF` header prefix). Must be called after Tokenize or will
// return 0.
type lexResult struct {
	version uint64
	tokens  []Token
}

// Tokenize decompresses r (gzip) and returns the SLF version and the full
// token slice. Memory peak = decompressed size, matching the pre-existing
// regex reader's cost model.
func Tokenize(r io.Reader) (version uint64, tokens []Token, err error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return 0, nil, fmt.Errorf("slf: gzip open: %w", err)
	}
	defer gz.Close()

	body, err := io.ReadAll(gz)
	if err != nil {
		return 0, nil, fmt.Errorf("slf: read gzip body: %w", err)
	}

	res, err := tokenizeBytes(body)
	if err != nil {
		return 0, nil, err
	}

	return res.version, res.tokens, nil
}

// TokenizeBytes is Tokenize over an already-decompressed byte slice.
// Exported for synthetic fixtures.
func TokenizeBytes(body []byte) (uint64, []Token, error) {
	res, err := tokenizeBytes(body)
	if err != nil {
		return 0, nil, err
	}

	return res.version, res.tokens, nil
}

func tokenizeBytes(body []byte) (lexResult, error) {
	l := &Lexer{buf: body}

	if !l.consumeHeader() {
		return lexResult{}, ErrInvalidHeader
	}

	tokens := make([]Token, 0, 1024)

	for l.offset < len(l.buf) {
		toks, err := l.scanOne()
		if err != nil {
			return lexResult{}, err
		}

		tokens = append(tokens, toks...)
	}

	if len(tokens) == 0 {
		return lexResult{}, errors.New("slf: empty token stream")
	}

	// First token after the header is the SLF version integer.
	if tokens[0].Type != TokenInt {
		return lexResult{}, fmt.Errorf("slf: expected version int as first token, got %s", tokens[0])
	}

	return lexResult{version: tokens[0].IntValue, tokens: tokens[1:]}, nil
}

func (l *Lexer) consumeHeader() bool {
	const header = "SLF"
	if len(l.buf)-l.offset < len(header) {
		return false
	}

	if string(l.buf[l.offset:l.offset+len(header)]) != header {
		return false
	}

	l.offset += len(header)

	return true
}

// scanOne reads one SLF value (= an optional ASCII payload prefix followed by
// one or more type-delimiter chars). Returns a slice because a run of
// delimiters like `--` encodes multiple nulls in one go (empty payload).
func (l *Lexer) scanOne() ([]Token, error) {
	payload := l.scanPayload()

	delims := l.scanDelimiters()
	if len(delims) == 0 {
		return nil, fmt.Errorf("slf: no type delimiter at offset %d (payload=%q next=%q)",
			l.offset, payload, l.peek(16))
	}

	// String-type payload bytes may legally contain subsequent delimiter chars
	// (`"` and friends appear in file names and signatures). If the first
	// delimiter is `"`, the payload length prefix governs — rewind the extras.
	if len(delims) > 1 && delims[0] == byte(TokenString) {
		rewind := len(delims) - 1
		l.offset -= rewind
		delims = delims[:1]
	}

	out := make([]Token, 0, len(delims))

	for i, d := range delims {
		var usedPayload string
		if i == 0 {
			usedPayload = payload
		}

		tok, err := l.decodeToken(TokenType(d), usedPayload)
		if err != nil {
			return nil, err
		}

		out = append(out, tok)
	}

	return out, nil
}

func (l *Lexer) scanPayload() string {
	start := l.offset

	for l.offset < len(l.buf) {
		c := l.buf[l.offset]
		if !isHexPayloadByte(c) {
			break
		}

		l.offset++
	}

	return string(l.buf[start:l.offset])
}

func (l *Lexer) scanDelimiters() []byte {
	start := l.offset

	for l.offset < len(l.buf) {
		if !isDelimiter(l.buf[l.offset]) {
			break
		}

		l.offset++
	}

	return l.buf[start:l.offset]
}

func (l *Lexer) decodeToken(t TokenType, payload string) (Token, error) {
	switch t {
	case TokenInt:
		v, err := strconv.ParseUint(payload, 10, 64)
		if err != nil {
			return Token{}, fmt.Errorf("slf: bad int payload %q at offset %d", payload, l.offset)
		}

		return Token{Type: TokenInt, IntValue: v}, nil

	case TokenDouble:
		// Little-endian hex of IEEE-754 bits.
		raw, err := strconv.ParseUint(payload, 16, 64)
		if err != nil {
			return Token{}, fmt.Errorf("slf: bad double payload %q at offset %d", payload, l.offset)
		}

		return Token{Type: TokenDouble, DoubleValue: math.Float64frombits(swapU64(raw))}, nil

	case TokenString:
		s, err := l.readString(payload)
		if err != nil {
			return Token{}, err
		}

		return Token{Type: TokenString, StringValue: s}, nil

	case TokenJSON:
		s, err := l.readString(payload)
		if err != nil {
			return Token{}, err
		}

		return Token{Type: TokenJSON, StringValue: s}, nil

	case TokenClassName:
		name, err := l.readString(payload)
		if err != nil {
			return Token{}, err
		}

		l.classNames = append(l.classNames, name)

		return Token{Type: TokenClassName, StringValue: name}, nil

	case TokenClassNameRef:
		idx, err := strconv.Atoi(payload)
		if err != nil {
			return Token{}, fmt.Errorf("slf: bad classNameRef payload %q", payload)
		}

		if idx < 1 || idx > len(l.classNames) {
			return Token{}, fmt.Errorf("slf: classNameRef %d out of range (seen %d names)", idx, len(l.classNames))
		}

		return Token{Type: TokenClassNameRef, StringValue: l.classNames[idx-1]}, nil

	case TokenNull:
		return Token{Type: TokenNull}, nil

	case TokenList:
		n, err := strconv.Atoi(payload)
		if err != nil {
			return Token{}, fmt.Errorf("slf: bad list payload %q", payload)
		}

		return Token{Type: TokenList, ListCount: n}, nil

	default:
		return Token{}, fmt.Errorf("slf: unknown delimiter %q at offset %d", byte(t), l.offset)
	}
}

// readString consumes exactly `count` bytes from the tape, where count is the
// NSString length (UTF-16 code units) per the SLF spec. Live Xcode logs are
// written in a mix of ASCII and UTF-8 — in practice count matches a byte
// slice as long as the content stays ASCII. For non-ASCII content UTF-16
// units can under-count byte length; we treat it as a byte count here, which
// matches XCLogParser's own ASCII-string loading step
// (`String(data: data, encoding: .ascii)`).
func (l *Lexer) readString(payload string) (string, error) {
	n, err := strconv.Atoi(payload)
	if err != nil {
		return "", fmt.Errorf("slf: bad string length %q", payload)
	}

	if n < 0 || l.offset+n > len(l.buf) {
		return "", fmt.Errorf("slf: string length %d overruns tape (offset=%d size=%d)", n, l.offset, len(l.buf))
	}

	s := string(l.buf[l.offset : l.offset+n])
	l.offset += n

	return s, nil
}

func (l *Lexer) peek(n int) string {
	end := l.offset + n
	if end > len(l.buf) {
		end = len(l.buf)
	}

	return string(l.buf[l.offset:end])
}

func isHexPayloadByte(c byte) bool {
	switch {
	case c >= '0' && c <= '9':
		return true
	case c >= 'a' && c <= 'f':
		return true
	}

	return false
}

func isDelimiter(c byte) bool {
	switch TokenType(c) {
	case TokenInt, TokenClassName, TokenClassNameRef, TokenString,
		TokenDouble, TokenNull, TokenList, TokenJSON:
		return true
	}

	return false
}

func swapU64(v uint64) uint64 {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)

	return binary.LittleEndian.Uint64(b[:])
}
