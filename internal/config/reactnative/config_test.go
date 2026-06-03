//go:build unit

package reactnative_test

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

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

	saved := reactnative.Config{Enabled: true}

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
