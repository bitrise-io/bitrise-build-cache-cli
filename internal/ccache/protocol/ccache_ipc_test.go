//go:build unit

package protocol_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache/protocol"
)

func Test_WriteReadGreeting(t *testing.T) {
	t.Run("roundtrip succeeds", func(t *testing.T) {
		var buf bytes.Buffer
		err := protocol.WriteGreeting(&buf)
		require.NoError(t, err)

		err = protocol.ReadGreeting(&buf)
		assert.NoError(t, err)
	})

	t.Run("wrong version returns error", func(t *testing.T) {
		// Write a greeting with wrong version
		var buf bytes.Buffer
		buf.WriteByte(0xFF) // wrong version
		buf.WriteByte(0x00) // 0 caps

		err := protocol.ReadGreeting(&buf)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported protocol version")
	})
}

func Test_WriteReadValue(t *testing.T) {
	t.Run("roundtrip preserves bytes", func(t *testing.T) {
		data := []byte("hello, world!")
		var buf bytes.Buffer

		err := protocol.WriteValue(&buf, data)
		require.NoError(t, err)

		got, err := protocol.ReadValue(&buf)
		require.NoError(t, err)
		assert.Equal(t, data, got)
	})

	t.Run("empty value roundtrip", func(t *testing.T) {
		var buf bytes.Buffer
		err := protocol.WriteValue(&buf, []byte{})
		require.NoError(t, err)

		got, err := protocol.ReadValue(&buf)
		require.NoError(t, err)
		assert.Equal(t, []byte{}, got)
	})
}

func Test_WriteReadMsg(t *testing.T) {
	t.Run("roundtrip succeeds", func(t *testing.T) {
		msg := "hello"
		var buf bytes.Buffer

		err := protocol.WriteMsg(&buf, msg)
		require.NoError(t, err)

		got, err := protocol.ReadMsg(&buf)
		require.NoError(t, err)
		assert.Equal(t, msg, got)
	})

	t.Run("message longer than 255 bytes is truncated to 255", func(t *testing.T) {
		msg := strings.Repeat("x", 300)
		var buf bytes.Buffer

		err := protocol.WriteMsg(&buf, msg)
		require.NoError(t, err)

		got, err := protocol.ReadMsg(&buf)
		require.NoError(t, err)
		assert.Equal(t, 255, len(got))
		assert.Equal(t, msg[:255], got)
	})
}

func Test_WriteSetInvocationID(t *testing.T) {
	t.Run("writes request type byte 0xB1 then parent and child IDs, ReadSetInvocationID reads them back", func(t *testing.T) {
		parentID := "my-parent-id"
		childID := "my-child-id"
		var buf bytes.Buffer

		err := protocol.WriteSetInvocationID(&buf, parentID, childID)
		require.NoError(t, err)

		// First byte should be RequestSetInvocationID (0xB1)
		firstByte, err := protocol.ReadByte(&buf)
		require.NoError(t, err)
		assert.Equal(t, byte(protocol.RequestSetInvocationID), firstByte)

		// Remainder should decode to parent and child IDs
		gotParent, gotChild, err := protocol.ReadSetInvocationID(&buf)
		require.NoError(t, err)
		assert.Equal(t, parentID, gotParent)
		assert.Equal(t, childID, gotChild)
	})
}

func Test_ReadKey(t *testing.T) {
	t.Run("reads length-prefixed key correctly", func(t *testing.T) {
		key := []byte{0xAB, 0xCD, 0xEF}
		var buf bytes.Buffer
		buf.WriteByte(byte(len(key)))
		buf.Write(key)

		got, err := protocol.ReadKey(&buf)
		require.NoError(t, err)
		assert.Equal(t, key, got)
	})
}

func Test_WriteOK(t *testing.T) {
	t.Run("writes response OK byte 0x00", func(t *testing.T) {
		var buf bytes.Buffer
		err := protocol.WriteOK(&buf)
		require.NoError(t, err)

		b, err := protocol.ReadByte(&buf)
		require.NoError(t, err)
		assert.Equal(t, byte(protocol.ResponseOK), b)
	})
}

func Test_WriteNoop(t *testing.T) {
	t.Run("writes response Noop byte 0x01", func(t *testing.T) {
		var buf bytes.Buffer
		err := protocol.WriteNoop(&buf)
		require.NoError(t, err)

		b, err := protocol.ReadByte(&buf)
		require.NoError(t, err)
		assert.Equal(t, byte(protocol.ResponseNoop), b)
	})
}

func Test_WriteErr(t *testing.T) {
	t.Run("writes response Err byte 0x02 and message", func(t *testing.T) {
		var buf bytes.Buffer
		errMsg := "something went wrong"
		err := protocol.WriteErr(&buf, errMsg)
		require.NoError(t, err)

		b, err := protocol.ReadByte(&buf)
		require.NoError(t, err)
		assert.Equal(t, byte(protocol.ResponseErr), b)

		got, err := protocol.ReadMsg(&buf)
		require.NoError(t, err)
		assert.Equal(t, errMsg, got)
	})
}
