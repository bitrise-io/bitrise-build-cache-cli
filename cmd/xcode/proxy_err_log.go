package xcode

import (
	"io"
	"os"
	"regexp"
	"strings"
)

const proxyErrorSnippetMax = 160

// readProxyStderrSince reads from offset because the log outlives a single build:
// reading it whole would report failures from previous ones.
func readProxyStderrSince(path string, offset int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return ""
	}

	buf, err := io.ReadAll(io.LimitReader(f, 8*1024))
	if err != nil {
		return ""
	}

	return strings.TrimSpace(stripANSI(string(buf)))
}

// fileSize is 0 for a missing file, so a first build reads its stderr from the start.
func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}

	return st.Size()
}

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

func stripANSI(s string) string { return ansiRegex.ReplaceAllString(s, "") }

func truncate(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}

	return s[:maxLen] + "…"
}
