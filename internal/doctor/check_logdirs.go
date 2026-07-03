package doctor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type logDirOutcome struct {
	Missing     string
	NotWritable string
	WrongOwner  string
	Fatal       error
}

func checkLogDir(path string) logDirOutcome {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return logDirOutcome{Missing: path}
		}

		return logDirOutcome{Fatal: fmt.Errorf("stat %s: %w", path, err)}
	}

	if !info.IsDir() {
		return logDirOutcome{NotWritable: path + " (not a directory)"}
	}

	probe := filepath.Join(path, ".doctor-probe")
	if werr := os.WriteFile(probe, []byte{}, 0o600); werr != nil {
		if statT, ok := info.Sys().(*syscall.Stat_t); ok {
			if int(statT.Uid) != os.Geteuid() {
				return logDirOutcome{WrongOwner: path}
			}
		}

		return logDirOutcome{NotWritable: path + " (" + werr.Error() + ")"}
	}
	_ = os.Remove(probe)

	return logDirOutcome{}
}

type logDirsSummary struct {
	Missing     []string
	NotWritable []string
	WrongOwner  []string
	Fatal       error
}

func collectLogDirState(candidates []string) logDirsSummary {
	var s logDirsSummary
	for _, path := range candidates {
		out := checkLogDir(path)
		if out.Fatal != nil {
			s.Fatal = out.Fatal

			return s
		}
		if out.Missing != "" {
			s.Missing = append(s.Missing, out.Missing)
		}
		if out.NotWritable != "" {
			s.NotWritable = append(s.NotWritable, out.NotWritable)
		}
		if out.WrongOwner != "" {
			s.WrongOwner = append(s.WrongOwner, out.WrongOwner)
		}
	}

	return s
}

func resultFromLogDirsSummary(s logDirsSummary) Result {
	if s.Fatal != nil {
		return Result{State: StateError, Detail: s.Fatal.Error()}
	}
	if len(s.WrongOwner) > 0 {
		return Result{
			State: StateError,
			Detail: fmt.Sprintf(
				"owned by another user (likely root from a previous sudo run): %s — run `sudo chown -R $(whoami) %s` to repair",
				strings.Join(s.WrongOwner, ", "),
				strings.Join(s.WrongOwner, " "),
			),
		}
	}
	if len(s.NotWritable) > 0 {
		return Result{State: StateError, Detail: "not writable: " + strings.Join(s.NotWritable, ", ")}
	}
	if len(s.Missing) > 0 {
		return Result{
			State:   StateWarn,
			Detail:  "missing: " + strings.Join(s.Missing, ", ") + " — fixable",
			Fixable: true,
			Fixer:   LogDirsFixer{Candidates: s.Missing},
		}
	}

	return Result{State: StateOK, Detail: "all log dirs present + writable"}
}

func (d *Doctor) logDirsCheck() Check {
	return Check{
		Name: "log-dirs",
		Diagnose: func(_ context.Context) Result {
			return resultFromLogDirsSummary(collectLogDirState(d.StateDirCandidates))
		},
	}
}
