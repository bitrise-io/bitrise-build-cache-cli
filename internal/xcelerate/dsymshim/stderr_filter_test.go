//go:build unit

package dsymshim

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStderrFilter_PairedWarningStripped(t *testing.T) {
	var out bytes.Buffer
	f := NewStderrFilter(&out)

	in := "warning: 0~UbC48bT5nebz4ClxEEHAyqx2oRoSNimMTKeHXt6Z2daJR6NVBgQ_EWnyaWB7xWvCbHvNHamO59dcOqwQEM8N-w==: No such file or directory\n" +
		"note: while processing 0~UbC48bT5nebz4ClxEEHAyqx2oRoSNimMTKeHXt6Z2daJR6NVBgQ_EWnyaWB7xWvCbHvNHamO59dcOqwQEM8N-w==\n" +
		"warning: 0~MG5jnBmhsB8v0-Njg3jioj2uKpDM-hM9Ou26AT2OXl2mLra_jxH1T2i2C-N1ajYze2iTZ_H6VXAIxkTAG3LcIg==: No such file or directory\n" +
		"note: while processing 0~MG5jnBmhsB8v0-Njg3jioj2uKpDM-hM9Ou26AT2OXl2mLra_jxH1T2i2C-N1ajYze2iTZ_H6VXAIxkTAG3LcIg==\n"

	n, err := f.Write([]byte(in))
	require.NoError(t, err)
	assert.Equal(t, len(in), n)

	result := f.Close()
	assert.Empty(t, out.String())
	assert.Equal(t, 2, result.FilteredPaired)
	assert.Equal(t, 2, result.ObservedCASIDs)
}

func TestStderrFilter_UnrelatedLinesPassthrough(t *testing.T) {
	var out bytes.Buffer
	f := NewStderrFilter(&out)

	in := "warning: unrelated dsymutil issue\n" +
		"note: a general note\n" +
		"info: processing something\n"

	_, err := f.Write([]byte(in))
	require.NoError(t, err)
	f.Close()

	assert.Equal(t, in, out.String())
}

func TestStderrFilter_WarningWithoutNoteFlushes(t *testing.T) {
	var out bytes.Buffer
	f := NewStderrFilter(&out)

	in := "warning: 0~ABC==: No such file or directory\n" +
		"other: next-line-is-not-a-note\n"

	_, err := f.Write([]byte(in))
	require.NoError(t, err)
	f.Close()

	// The paired strip only fires on the exact note. Unpaired warning must survive.
	assert.Contains(t, out.String(), "warning: 0~ABC==: No such file or directory\n")
	assert.Contains(t, out.String(), "other: next-line-is-not-a-note\n")
}

func TestStderrFilter_HandlesChunkedWrites(t *testing.T) {
	var out bytes.Buffer
	f := NewStderrFilter(&out)

	// Write the paired warning+note split across many byte boundaries to confirm
	// the line-oriented filter does not misfire mid-line.
	full := "warning: 0~XYZ==: No such file or directory\nnote: while processing 0~XYZ==\n"
	for i := 0; i < len(full); i++ {
		_, err := f.Write([]byte{full[i]})
		require.NoError(t, err)
	}

	result := f.Close()
	assert.Empty(t, out.String())
	assert.Equal(t, 1, result.FilteredPaired)
}

func TestStderrFilter_PathPrefixedWarningVariantStripped(t *testing.T) {
	var out bytes.Buffer
	f := NewStderrFilter(&out)

	in := "warning: /Users/vagrant/git/Demo/AmerigoDemo.xcodeproj/0~ojZxUvY4KDCc7Wi5mBY_rihACosJeJ1YBvM_rv51zHR8VW8Ce381hGC_tTgeDHxA-mL-5FGc-F_Hg4e1kEI88Q==: No such file or directory\n" +
		"note: while processing /Users/vagrant/git/Demo/AmerigoDemo.xcodeproj/0~ojZxUvY4KDCc7Wi5mBY_rihACosJeJ1YBvM_rv51zHR8VW8Ce381hGC_tTgeDHxA-mL-5FGc-F_Hg4e1kEI88Q==\n"

	_, err := f.Write([]byte(in))
	require.NoError(t, err)

	result := f.Close()
	assert.Empty(t, out.String())
	assert.Equal(t, 1, result.FilteredPaired)
}

func TestStderrFilter_UnpairedTrailingWarningFlushed(t *testing.T) {
	var out bytes.Buffer
	f := NewStderrFilter(&out)

	// Process ends with a warning that was never followed by a note. The warning
	// still has to reach the user; the filter never swallows single lines.
	_, err := f.Write([]byte("warning: 0~ABC==: No such file or directory\n"))
	require.NoError(t, err)

	result := f.Close()
	assert.Equal(t, "warning: 0~ABC==: No such file or directory\n", out.String())
	assert.Equal(t, 0, result.FilteredPaired)
	assert.Equal(t, 1, result.ObservedCASIDs)
}
