package xcode

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// Word-anchored so ordinary lines ("hit: false", a key that spells a keyword)
// don't register.
var proxyErrorLineRegex = regexp.MustCompile(`(?i)\b(error|failed|refused|unavailable|unauthenticated|deadline ?exceeded|permission denied)\b`)

const (
	// A broken backend produces thousands of near-identical lines.
	proxyErrorSampleMax  = 3
	proxyErrorSnippetMax = 160
)

type proxyLogFindings struct {
	Errors  int
	Samples []string
	// Stderr is what the proxy wrote to its shared error log during this build,
	// which is where a startup failure shows up.
	Stderr string
}

func (f proxyLogFindings) any() bool {
	return f.Errors > 0 || f.Stderr != ""
}

func scanProxyLog(r io.Reader) proxyLogFindings {
	var out proxyLogFindings

	seen := map[string]bool{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := stripANSI(scanner.Text())
		if !proxyErrorLineRegex.MatchString(line) {
			continue
		}
		out.Errors++

		if len(out.Samples) >= proxyErrorSampleMax {
			continue
		}
		shape := proxyErrorShape(line)
		if seen[shape] {
			continue
		}
		seen[shape] = true
		out.Samples = append(out.Samples, truncate(line, proxyErrorSnippetMax))
	}

	return out
}

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

var (
	ansiRegex = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)
	hexRegex  = regexp.MustCompile(`[0-9a-f]{8,}`)
)

func stripANSI(s string) string { return ansiRegex.ReplaceAllString(s, "") }

// proxyErrorShape collapses per-request identifiers so messages differing only
// by cache key count as one sample.
func proxyErrorShape(line string) string {
	return hexRegex.ReplaceAllString(line, "<id>")
}

func truncate(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}

	return s[:maxLen] + "…"
}

func (f proxyLogFindings) summary() string {
	if f.Errors == 0 {
		return "the proxy logged errors"
	}

	return fmt.Sprintf("the proxy logged %d error line(s)", f.Errors)
}
