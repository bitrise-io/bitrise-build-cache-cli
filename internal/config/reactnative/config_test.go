//go:build unit

package reactnative_test

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/reactnative"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
	utilsMocks "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils/mocks"
)

func TestConfig_SaveAndRead_RoundTrip(t *testing.T) {
	temp := t.TempDir()

	osProxy := &utilsMocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return temp, nil },
		MkdirAllFunc:    os.MkdirAll,
		CreateFunc:      os.Create,
		OpenFileFunc:    os.OpenFile,
	}

	encoderFactory := &utilsMocks.EncoderFactoryMock{
		EncoderFunc: func(w io.Writer) utils.Encoder { return json.NewEncoder(w) },
	}
	decoderFactory := &utilsMocks.DecoderFactoryMock{
		DecoderFunc: func(r io.Reader) utils.Decoder { return json.NewDecoder(r) },
	}

	saved := reactnative.Config{
		Version:     "1.2.3",
		ActivatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Enabled:     true,
		Gradle:      true,
		Xcode:       true,
		Cpp:         false,
	}

	err := saved.Save(mockLogger, osProxy, encoderFactory)
	require.NoError(t, err)

	loaded, err := reactnative.ReadConfig(osProxy, decoderFactory)
	require.NoError(t, err)

	assert.Equal(t, saved, loaded)
	assert.FileExists(t, filepath.Join(temp, ".bitrise/cache/reactnative/config.json"))
}

func TestReadConfig_MissingFile(t *testing.T) {
	temp := t.TempDir()

	osProxy := &utilsMocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return temp, nil },
		OpenFileFunc:    os.OpenFile,
	}
	decoderFactory := &utilsMocks.DecoderFactoryMock{
		DecoderFunc: func(r io.Reader) utils.Decoder { return json.NewDecoder(r) },
	}

	_, err := reactnative.ReadConfig(osProxy, decoderFactory)
	require.Error(t, err)
}

func TestRemove(t *testing.T) {
	temp := t.TempDir()
	dir := filepath.Join(temp, ".bitrise/cache/reactnative")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, reactnative.ConfigFileName)
	require.NoError(t, os.WriteFile(file, []byte("{}"), 0o600))

	osProxy := &utilsMocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return temp, nil },
		RemoveFunc:      os.Remove,
	}

	require.NoError(t, reactnative.Remove(osProxy))
	_, err := os.Stat(file)
	assert.True(t, os.IsNotExist(err))
}
