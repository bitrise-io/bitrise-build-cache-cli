//go:build unit

package slf_test

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/slf"
)

// TestTokenize_AllTokenTypes exercises each of the eight SLF tokens plus
// the gzip wrapper. Confirms:
//   - int, null, list, string, double, className, classNameRef, JSON all decode;
//   - string length prefix bounds a literal `*` from being mis-read as JSON;
//   - double tokens round-trip through the little-endian hex encoding.
func TestTokenize_AllTokenTypes(t *testing.T) {
	body := buildSyntheticTokens(t)

	wrapped := gzipped(t, body)

	version, tokens, err := slf.Tokenize(bytes.NewReader(wrapped))
	require.NoError(t, err)
	assert.Equal(t, uint64(11), version)

	// First post-version tokens are: int(42), null, list(2), string("hi"),
	// double(1.5), className("Foo"), classNameRef("Foo"), json({"k":1}).
	require.GreaterOrEqual(t, len(tokens), 8)

	assert.Equal(t, slf.TokenInt, tokens[0].Type)
	assert.Equal(t, uint64(42), tokens[0].IntValue)

	assert.Equal(t, slf.TokenNull, tokens[1].Type)

	assert.Equal(t, slf.TokenList, tokens[2].Type)
	assert.Equal(t, 2, tokens[2].ListCount)

	assert.Equal(t, slf.TokenString, tokens[3].Type)
	assert.Equal(t, "hi", tokens[3].StringValue)

	assert.Equal(t, slf.TokenDouble, tokens[4].Type)
	assert.InDelta(t, 1.5, tokens[4].DoubleValue, 0.0001)

	assert.Equal(t, slf.TokenClassName, tokens[5].Type)
	assert.Equal(t, "Foo", tokens[5].StringValue)

	assert.Equal(t, slf.TokenClassNameRef, tokens[6].Type)
	assert.Equal(t, "Foo", tokens[6].StringValue, "classNameRef must resolve to the registered class name")

	assert.Equal(t, slf.TokenJSON, tokens[7].Type)
	assert.Equal(t, `{"k":1}`, tokens[7].StringValue)
}

// TestTokenize_InvalidHeader surfaces the sentinel error.
func TestTokenize_InvalidHeader(t *testing.T) {
	wrapped := gzipped(t, []byte("NOTSLF11#"))

	_, _, err := slf.Tokenize(bytes.NewReader(wrapped))
	require.ErrorIs(t, err, slf.ErrInvalidHeader)
}

// TestTokenize_StringContainingDelimiter proves the length-prefix on strings
// shields subsequent `"` / `-` / etc. bytes from being parsed as fresh
// delimiters — the exact edge case the Swift upstream calls out.
func TestTokenize_StringContainingDelimiter(t *testing.T) {
	// Length-5 string whose body contains a `-` and a `*`, followed by a null.
	body := []byte("SLF11#5\"he-*l-")

	wrapped := gzipped(t, body)

	version, tokens, err := slf.Tokenize(bytes.NewReader(wrapped))
	require.NoError(t, err)
	assert.Equal(t, uint64(11), version)
	require.Len(t, tokens, 2)

	assert.Equal(t, slf.TokenString, tokens[0].Type)
	assert.Equal(t, "he-*l", tokens[0].StringValue)

	assert.Equal(t, slf.TokenNull, tokens[1].Type)
}

func gzipped(t *testing.T, body []byte) []byte {
	t.Helper()

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	_, err := gz.Write(body)
	require.NoError(t, err)
	require.NoError(t, gz.Close())

	return buf.Bytes()
}

// buildSyntheticTokens emits a tiny SLF v11 stream covering every token type.
func buildSyntheticTokens(t *testing.T) []byte {
	t.Helper()

	var b bytes.Buffer

	b.WriteString("SLF11#")  // header + version=11
	b.WriteString("42#")     // int(42)
	b.WriteString("-")       // null
	b.WriteString("2(")      // list(2)
	b.WriteString("2\"hi")   // string("hi")
	b.WriteString(encodeDouble(1.5) + "^")
	b.WriteString("3%Foo")   // className("Foo")
	b.WriteString("1@")      // classNameRef(1) -> "Foo"
	b.WriteString(`7*{"k":1}`)

	return b.Bytes()
}

// encodeDouble mirrors `swapU64` + hex encoding — big-endian bytes of the
// IEEE-754 bits, printed as lowercase hex.
func encodeDouble(f float64) string {
	bits := math.Float64bits(f)

	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], bits)

	// SLF format: little-endian bytes in hex. Swap bytes to produce the
	// little-endian layout then hex-encode.
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}

	hex := make([]byte, 16)
	const hexdigits = "0123456789abcdef"

	for i, by := range buf {
		hex[i*2] = hexdigits[by>>4]
		hex[i*2+1] = hexdigits[by&0x0f]
	}

	return string(hex)
}

// TestAttachment_RawJSONPreserved asserts that unknown-identifier attachments
// still carry the raw JSON body, even though no typed field is populated.
// Verifies the forward-compat path for an Xcode major that invents a new
// attachment identifier.
func TestAttachment_RawJSONPreserved(t *testing.T) {
	// Minimal section-shaped stream with ONE attachment of an unknown ID.
	body := buildStreamWithAttachment(t, "com.example.custom.ActivityLogSectionAttachment.NovelThing", `{"foo":42}`)

	wrapped := gzipped(t, body)

	root, version, err := slf.Decode(bytes.NewReader(wrapped))
	require.NoError(t, err)
	assert.Equal(t, uint64(11), version)
	require.NotNil(t, root)
	require.Len(t, root.Attachments, 1)

	a := root.Attachments[0]
	assert.Equal(t, "com.example.custom.ActivityLogSectionAttachment.NovelThing", a.Identifier)
	assert.JSONEq(t, `{"foo":42}`, string(a.Payload), "unknown identifier still preserves the raw JSON body")
	assert.Nil(t, a.BuildOp)
	assert.Nil(t, a.TaskMetrics)
	assert.Nil(t, a.TaskBacktrace)
}

// buildStreamWithAttachment emits a hand-crafted SLF v11 stream containing a
// single minimal IDEActivityLogSection with one attachment. The section's
// property tape is kept to the bare minimum needed to satisfy the decoder.
func buildStreamWithAttachment(t *testing.T, identifier, jsonBody string) []byte {
	t.Helper()

	payloadLen := strconv.Itoa(len(jsonBody))

	var b bytes.Buffer

	b.WriteString("SLF11#")

	// Section header: className + classNameRef.
	b.WriteString("21%IDEActivityLogSection")
	b.WriteString("1@")

	// Base properties: sectionType, domainType, title, signature, time*, subSections.
	b.WriteString("0#")       // sectionType = 0
	b.WriteString("3\"dom")   // domainType = "dom"
	b.WriteString("3\"tit")   // title = "tit"
	b.WriteString("3\"sig")   // signature = "sig"
	b.WriteString(encodeDouble(100) + "^")
	b.WriteString(encodeDouble(200) + "^")
	b.WriteString("-")        // subSections = null

	b.WriteString("3\"txt")   // text = "txt"
	b.WriteString("-")        // messages = null
	b.WriteString("0#")       // wasCancelled = false
	b.WriteString("0#")       // isQuiet = false
	b.WriteString("1#")       // wasFetchedFromCache = true
	b.WriteString("3\"sub")   // subtitle = "sub"
	b.WriteString("-")        // location = null
	b.WriteString("3\"cmd")   // commandDetailDesc = "cmd"
	b.WriteString("3\"uid")   // uniqueIdentifier = "uid"
	b.WriteString("3\"loc")   // localizedResultString = "loc"
	b.WriteString("3\"xcb")   // xcbuildSignature = "xcb"

	// Attachments list (SLF v11+).
	const attachmentClass = "IDEFoundation.IDEActivityLogSectionAttachment"

	b.WriteString("1(")
	b.WriteString(strconv.Itoa(len(attachmentClass)) + "%" + attachmentClass)
	b.WriteString("2@") // classNameRef index 2
	b.WriteString(strconv.Itoa(len(identifier)) + "\"" + identifier)
	b.WriteString("1#") // majorVersion
	b.WriteString("0#") // minorVersion
	b.WriteString(payloadLen + "*" + jsonBody)

	return b.Bytes()
}
