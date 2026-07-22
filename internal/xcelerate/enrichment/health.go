package enrichment

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type HealthSnapshot struct {
	LastAttempt       time.Time `json:"last_attempt"`
	LastSuccess       time.Time `json:"last_success"`
	LastError         string    `json:"last_error,omitempty"`
	LastErrorAt       time.Time `json:"last_error_at,omitempty"`
	ConsecutiveErrors int       `json:"consecutive_errors"`
}

type HealthWriter struct {
	Path string

	mu sync.Mutex
}

func (h *HealthWriter) Update(mutate func(*HealthSnapshot)) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	snap, err := LoadHealth(h.Path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	mutate(&snap)

	return h.writeAtomic(snap)
}

func (h *HealthWriter) writeAtomic(snap HealthSnapshot) error {
	if err := os.MkdirAll(filepath.Dir(h.Path), 0o755); err != nil {
		return fmt.Errorf("mkdir health dir: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(h.Path), ".health-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()

	defer func() {
		_ = os.Remove(tmpPath)
	}()

	w := bufio.NewWriter(tmp)
	if err := json.NewEncoder(w).Encode(snap); err != nil {
		tmp.Close()

		return fmt.Errorf("encode health: %w", err)
	}

	if err := w.Flush(); err != nil {
		tmp.Close()

		return fmt.Errorf("flush health: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}

	if err := os.Rename(tmpPath, h.Path); err != nil {
		return fmt.Errorf("rename health: %w", err)
	}

	return nil
}

func LoadHealth(path string) (HealthSnapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return HealthSnapshot{}, err //nolint:wrapcheck // callers rely on os.ErrNotExist sentinel
	}
	defer f.Close()

	var snap HealthSnapshot
	if err := json.NewDecoder(f).Decode(&snap); err != nil {
		return HealthSnapshot{}, fmt.Errorf("decode health: %w", err)
	}

	return snap, nil
}
