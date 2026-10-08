//go:build unit

package xcelerate

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

type stubOsProxy struct {
	utils.DefaultOsProxy

	home string
}

func (s stubOsProxy) UserHomeDir() (string, error) { return s.home, nil }

func TestAnnounceOrphanInvocations_PrintsVisitURL(t *testing.T) {
	home := t.TempDir()
	p := paths.FromHome(home)
	require.NoError(t, os.MkdirAll(p.XcelerateLogDir(), 0o755))

	log := filepath.Join(p.XcelerateLogDir(), "proxy-123-out.log")
	body := "random prelude\n" +
		"[INFO] Enriched invocation PUT abc12345-6789-4abc-9def-012345678900 (orphan ...)\n" +
		"other stuff\n" +
		"Enriched invocation PUT abc12345-6789-4abc-9def-012345678900 duplicate\n"
	require.NoError(t, os.WriteFile(log, []byte(body), 0o600))

	nullLogger := nullLogger{}
	buf := &bytes.Buffer{}
	announceOrphanInvocations(stubOsProxy{home: home}, time.Now().Add(-time.Hour), buf, nullLogger)

	out := buf.String()
	assert.Equal(t, 1, strings.Count(out, "Invocation saved. Visit"))
	assert.Contains(t, out, "https://app.bitrise.io/build-cache/invocations/xcode/abc12345-6789-4abc-9def-012345678900")
}

func TestAnnounceOrphanInvocations_SkipsOlderLogFiles(t *testing.T) {
	home := t.TempDir()
	p := paths.FromHome(home)
	require.NoError(t, os.MkdirAll(p.XcelerateLogDir(), 0o755))

	log := filepath.Join(p.XcelerateLogDir(), "proxy-old-out.log")
	require.NoError(t, os.WriteFile(log, []byte("Enriched invocation PUT ff\n"), 0o600))

	past := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(log, past, past))

	nullLogger := nullLogger{}
	buf := &bytes.Buffer{}
	announceOrphanInvocations(stubOsProxy{home: home}, time.Now(), buf, nullLogger)

	assert.Empty(t, buf.String(), "log files older than the stop signal must not contribute Visit URLs")
}

type nullLogger struct{}

func (nullLogger) Printf(string, ...any)                    {}
func (nullLogger) Donef(string, ...any)                     {}
func (nullLogger) Infof(string, ...any)                     {}
func (nullLogger) Warnf(string, ...any)                     {}
func (nullLogger) Errorf(string, ...any)                    {}
func (nullLogger) Debugf(string, ...any)                    {}
func (nullLogger) TPrintf(string, ...any)                   {}
func (nullLogger) TDonef(string, ...any)                    {}
func (nullLogger) TInfof(string, ...any)                    {}
func (nullLogger) TWarnf(string, ...any)                    {}
func (nullLogger) TErrorf(string, ...any)                   {}
func (nullLogger) TDebugf(string, ...any)                   {}
func (nullLogger) Println()                                 {}
func (nullLogger) EnableDebugLog(bool)                      {}
