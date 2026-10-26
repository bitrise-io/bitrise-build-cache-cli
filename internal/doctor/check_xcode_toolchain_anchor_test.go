//go:build unit

package doctor

import (
	"errors"
	"io/fs"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils/mocks"
)

func TestDiagnoseXcodeToolchainAnchor_anchorMissingWarns(t *testing.T) {
	osProxy := &mocks.OsProxyMock{
		LstatFunc: func(_ string) (os.FileInfo, error) {
			return nil, &fs.PathError{Op: "lstat", Err: fs.ErrNotExist}
		},
	}

	res := diagnoseXcodeToolchainAnchor(osProxy)

	assert.Equal(t, StateWarn, res.State)
	assert.Contains(t, res.Detail, "/var/db/xcode_select_link")
	assert.Contains(t, res.Detail, "missing")
}

func TestDiagnoseXcodeToolchainAnchor_anchorDanglingWarns(t *testing.T) {
	// Lstat succeeds (link present) but Stat fails with NotExist (target gone).
	osProxy := &mocks.OsProxyMock{
		LstatFunc: func(_ string) (os.FileInfo, error) {
			return nil, nil
		},
		ReadlinkFunc: func(_ string) (string, error) {
			return "/Applications/Xcode-26.app/Contents/Developer", nil
		},
		StatFunc: func(_ string) (os.FileInfo, error) {
			return nil, &fs.PathError{Op: "stat", Err: fs.ErrNotExist}
		},
	}

	res := diagnoseXcodeToolchainAnchor(osProxy)

	assert.Equal(t, StateWarn, res.State)
	assert.Contains(t, res.Detail, "dangling")
	assert.Contains(t, res.Detail, "/Applications/Xcode-26.app/Contents/Developer")
}

func TestDiagnoseXcodeToolchainAnchor_pluginMissingWarns(t *testing.T) {
	anchorPath := "/var/db/xcode_select_link"
	pluginPath := "/var/db/xcode_select_link/usr/lib/libToolchainCASPlugin.dylib"
	osProxy := &mocks.OsProxyMock{
		LstatFunc: func(_ string) (os.FileInfo, error) { return nil, nil },
		ReadlinkFunc: func(_ string) (string, error) {
			return "/Applications/Xcode.app/Contents/Developer", nil
		},
		StatFunc: func(name string) (os.FileInfo, error) {
			if name == anchorPath {
				return nil, nil
			}
			if name == pluginPath {
				return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
			}

			return nil, errors.New("unexpected stat " + name)
		},
	}

	res := diagnoseXcodeToolchainAnchor(osProxy)

	assert.Equal(t, StateWarn, res.State)
	assert.Contains(t, res.Detail, "CAS plugin missing")
	assert.Contains(t, res.Detail, "/Applications/Xcode.app/Contents/Developer")
}

func TestDiagnoseXcodeToolchainAnchor_okWhenAnchorAndPluginPresent(t *testing.T) {
	osProxy := &mocks.OsProxyMock{
		LstatFunc: func(_ string) (os.FileInfo, error) { return nil, nil },
		ReadlinkFunc: func(_ string) (string, error) {
			return "/Applications/Xcode.app/Contents/Developer", nil
		},
		StatFunc: func(_ string) (os.FileInfo, error) { return nil, nil },
	}

	res := diagnoseXcodeToolchainAnchor(osProxy)

	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "/var/db/xcode_select_link")
	assert.Contains(t, res.Detail, "/Applications/Xcode.app/Contents/Developer")
}
