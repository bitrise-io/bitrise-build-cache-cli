package xcode_app_casinject

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const CasConfigName = ".cas-config"

type remoteService struct {
	Path string `json:"Path"`
}

// InjectFile rewrites .cas-config at path so RemoteService.Path == socketPath.
// Returns (rewritten, error). rewritten=false means the file already had the
// desired RemoteService (idempotent no-op).
func InjectFile(path, socketPath string) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	patched, changed, err := patchCasConfig(raw, socketPath)
	if err != nil {
		return false, fmt.Errorf("patch %s: %w", path, err)
	}
	if !changed {
		return false, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return false, fmt.Errorf("stat %s: %w", path, err)
	}

	if err := atomicWrite(path, patched, info.Mode().Perm()); err != nil {
		return false, err
	}

	return true, nil
}

func patchCasConfig(raw []byte, socketPath string) ([]byte, bool, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, false, fmt.Errorf("unmarshal: %w", err)
	}

	if existing, ok := obj["RemoteService"]; ok {
		var rs remoteService
		if err := json.Unmarshal(existing, &rs); err == nil && rs.Path == socketPath {
			return nil, false, nil
		}
	}

	rsBytes, err := json.Marshal(remoteService{Path: socketPath})
	if err != nil {
		return nil, false, fmt.Errorf("marshal remote-service: %w", err)
	}
	obj["RemoteService"] = rsBytes

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(obj); err != nil {
		return nil, false, fmt.Errorf("encode: %w", err)
	}

	return bytes.TrimRight(buf.Bytes(), "\n"), true, nil
}

func atomicWrite(target string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".cas-config.tmp-*")
	if err != nil {
		return fmt.Errorf("tempfile: %w", err)
	}
	tmpPath := tmp.Name()

	cleanup := func() {
		_ = os.Remove(tmpPath)
	}

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()

		return fmt.Errorf("write: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		cleanup()

		return fmt.Errorf("chmod: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()

		return fmt.Errorf("close: %w", err)
	}

	if err := os.Rename(tmpPath, target); err != nil {
		cleanup()

		return fmt.Errorf("rename: %w", err)
	}

	return nil
}

// IsCasConfigPath returns true when the path's basename is ".cas-config".
func IsCasConfigPath(p string) bool {
	return filepath.Base(p) == CasConfigName
}

// ErrSocketMissing is returned by ValidateSocket when the socket does not exist.
var ErrSocketMissing = errors.New("proxy socket not found")

// ValidateSocket verifies the socket path is a Unix socket. Cheap sanity check
// before we start rewriting build files with it.
func ValidateSocket(path string) error {
	st, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrSocketMissing, path)
	}
	if err != nil {
		return fmt.Errorf("stat socket: %w", err)
	}
	if st.Mode()&fs.ModeSocket == 0 {
		return fmt.Errorf("not a unix socket: %s", path)
	}

	return nil
}
