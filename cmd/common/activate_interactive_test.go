//go:build unit

package common

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestPrompter(input string, secrets ...string) (*prompter, *bytes.Buffer) {
	out := &bytes.Buffer{}
	secretIdx := 0
	secretsCopy := append([]string{}, secrets...)

	return &prompter{
		reader: bufio.NewReader(strings.NewReader(input)),
		out:    out,
		readPassword: func() (string, error) {
			if secretIdx >= len(secretsCopy) {
				return "", nil
			}

			s := secretsCopy[secretIdx]
			secretIdx++

			return s, nil
		},
	}, out
}

func TestPromptTool_Selection(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  interactiveTool
	}{
		{name: "gradle", input: "1\n", want: toolGradle},
		{name: "bazel", input: "2\n", want: toolBazel},
		{name: "xcode", input: "3\n", want: toolXcode},
		{name: "retry on invalid then accept", input: "abc\n0\n4\n2\n", want: toolBazel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _ := newTestPrompter(tt.input)

			got, err := promptTool(p)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPromptRequiredLine(t *testing.T) {
	t.Run("trims and returns first non-empty", func(t *testing.T) {
		p, _ := newTestPrompter("   \n  ws-123  \n")

		got, err := promptRequiredLine(p, "Workspace ID")
		require.NoError(t, err)
		assert.Equal(t, "ws-123", got)
	})

	t.Run("errors on closed stdin with no value", func(t *testing.T) {
		p, _ := newTestPrompter("")

		_, err := promptRequiredLine(p, "Workspace ID")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Workspace ID")
	})
}

func TestPromptRequiredSecret(t *testing.T) {
	t.Run("returns first non-empty masked value", func(t *testing.T) {
		p, _ := newTestPrompter("", "", "  secret-token  ")

		got, err := promptRequiredSecret(p, "Auth token")
		require.NoError(t, err)
		assert.Equal(t, "secret-token", got)
	})
}

func TestPromptPushEnabled(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "default (empty) is pull-only", input: "\n", want: false},
		{name: "explicit 1 is pull-only", input: "1\n", want: false},
		{name: "2 enables push", input: "2\n", want: true},
		{name: "retry on invalid then accept default", input: "abc\n\n", want: false},
		{name: "retry on invalid then choose push", input: "9\n2\n", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _ := newTestPrompter(tt.input)

			got, err := promptPushEnabled(p)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestActivateCmd_HasInteractiveFlag(t *testing.T) {
	flag := ActivateCmd.Flags().Lookup("interactive")
	require.NotNil(t, flag, "--interactive flag should be registered on activate command")
	assert.Equal(t, "false", flag.DefValue)
}
