package mocks

import (
	"os"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

type MockOsProxy struct {
	ReadFileIfExists func(pth string) (string, bool, error)
	MkdirAll         func(pth string, perm os.FileMode) error
	WriteFile        func(name string, data []byte, perm os.FileMode) error
	UserHomeDir      func() (string, error)
	Create           func(name string) (*os.File, error)
}

func (mock MockOsProxy) Proxy() utils.OsProxy {
	return utils.OsProxy{
		ReadFileIfExists: mock.ReadFileIfExists,
		MkdirAll:         mock.MkdirAll,
		WriteFile:        mock.WriteFile,
		UserHomeDir:      mock.UserHomeDir,
		Create:           mock.Create,
	}
}
