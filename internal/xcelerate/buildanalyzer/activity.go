package buildanalyzer

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ReadReport decompresses a .xcactivitylog and returns a v0 Report with one
// Task per IDEActivityLogCommandInvocationSection found in the log.
func ReadReport(path string) (Report, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Report{}, fmt.Errorf("open xcactivitylog: %w", err)
		}

		return Report{}, fmt.Errorf("open xcactivitylog: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return Report{}, fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	// SLF payloads carry multi-MB rows; ReadAll matches internal/xcactivitylog.
	body, err := io.ReadAll(gz)
	if err != nil {
		return Report{}, fmt.Errorf("read xcactivitylog body: %w", err)
	}

	return parseReport(body)
}

// MostRecentActivityLog walks DerivedData/Logs/Build/*.xcactivitylog under
// derivedData and returns the newest by mtime. Empty string + no error when
// the directory holds no such file (callers decide whether that's fatal).
func MostRecentActivityLog(derivedData string) (string, error) {
	dir := derivedData + "/Logs/Build"

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", dir, err)
	}

	type candidate struct {
		path  string
		mtime time.Time
	}

	cands := make([]candidate, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".xcactivitylog") {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		cands = append(cands, candidate{path: dir + "/" + e.Name(), mtime: info.ModTime()})
	}

	if len(cands) == 0 {
		return "", nil
	}

	sort.Slice(cands, func(i, j int) bool { return cands[i].mtime.After(cands[j].mtime) })

	return cands[0].path, nil
}

// JoinManifest extends report in place with manifest-sourced data:
//   - matches each ran Task to a planned Command (by signature when both
//     carry one, else by description string normalized the same way) and
//     stamps Planned=true + overrides Tool + fills Inputs/Outputs.
//   - appends a synthetic Task (Ran=false, Planned=true) for every Command
//     that had no activity-log section.
//   - derives Dependency edges from the manifest's input/output file-path
//     join: when Command A outputs file F and Command B inputs F, emit
//     Dependency{From: A.Key, To: B.Key}.
//
// Commands with no description are orchestration artifacts (phony,
// stale-file-removal, create-build-directory …) that never produce an
// activity-log section; they're skipped entirely so the DAG stays readable.
func JoinManifest(report *Report, manifest Manifest) {
	// Index Commands by description + signature. Each commandEntry carries the
	// canonical Key the Report will use — the command map's key string, which
	// is what Graphviz nodes get labeled with.
	type commandEntry struct {
		key string
		cmd ManifestCommand
	}

	bySignature := make(map[string]commandEntry, len(manifest.Commands))
	byDescription := make(map[string]commandEntry, len(manifest.Commands))

	for key, cmd := range manifest.Commands {
		if cmd.Description == "" {
			continue
		}

		entry := commandEntry{key: key, cmd: cmd}
		if cmd.Signature != "" {
			bySignature[cmd.Signature] = entry
		}
		byDescription[cmd.Description] = entry
	}

	matched := make(map[string]struct{}, len(manifest.Commands))

	for i := range report.Tasks {
		task := &report.Tasks[i]

		stripped := stripInTargetSuffix(task.Key)
		var hit commandEntry
		var ok bool
		if entry, found := bySignature[task.Key]; found {
			hit, ok = entry, true
		} else if entry, found := byDescription[stripped]; found {
			hit, ok = entry, true
		}

		if !ok {
			continue
		}

		matched[hit.key] = struct{}{}

		task.Planned = true
		// Override v0's first-word tool heuristic with the manifest's llbuild
		// tool label — `linker`, `swift-driver-compilation`, `shell`, etc.
		// The manifest value is canonical; v0's extractTool is a prefix hack.
		task.Tool = hit.cmd.Tool
		task.Inputs = hit.cmd.Inputs
		task.Outputs = hit.cmd.Outputs
	}

	// Append planned-only Tasks so the dot renderer can render them greyed-out.
	// Walk commands in sorted key order for a stable append sequence.
	unmatchedKeys := make([]string, 0, len(manifest.Commands))
	for key, cmd := range manifest.Commands {
		if cmd.Description == "" {
			continue
		}
		if _, hit := matched[key]; hit {
			continue
		}

		unmatchedKeys = append(unmatchedKeys, key)
	}
	sort.Strings(unmatchedKeys)

	for _, key := range unmatchedKeys {
		cmd := manifest.Commands[key]
		report.Tasks = append(report.Tasks, Task{
			Key:     key,
			Tool:    cmd.Tool,
			Target:  extractTarget(cmd.Description),
			Planned: true,
			Ran:     false,
			Inputs:  cmd.Inputs,
			Outputs: cmd.Outputs,
		})
	}

	report.Dependencies = deriveDependencies(manifest)
}

// deriveDependencies walks the manifest commands and emits one edge per
// input/output file-path join. Orchestration commands (empty description) are
// skipped on both sides so the DAG surfaces real tasks only.
func deriveDependencies(manifest Manifest) []Dependency {
	// Index: file path -> list of command keys that produce it.
	producers := make(map[string][]string, len(manifest.Commands))

	for key, cmd := range manifest.Commands {
		if cmd.Description == "" {
			continue
		}

		for _, out := range cmd.Outputs {
			producers[out] = append(producers[out], key)
		}
	}

	seen := make(map[string]struct{}, len(manifest.Commands))
	deps := make([]Dependency, 0, len(manifest.Commands))

	for consumerKey, cmd := range manifest.Commands {
		if cmd.Description == "" {
			continue
		}

		for _, in := range cmd.Inputs {
			for _, producerKey := range producers[in] {
				if producerKey == consumerKey {
					continue
				}

				edgeID := producerKey + "\x00" + consumerKey
				if _, dup := seen[edgeID]; dup {
					continue
				}
				seen[edgeID] = struct{}{}

				deps = append(deps, Dependency{From: producerKey, To: consumerKey})
			}
		}
	}

	sort.Slice(deps, func(i, j int) bool {
		if deps[i].From != deps[j].From {
			return deps[i].From < deps[j].From
		}

		return deps[i].To < deps[j].To
	})

	return deps
}

// stripInTargetSuffix peels off ` (in target 'X' from project 'Y')` and any
// trailing whitespace. Activity-log signatures always carry the target
// clause; manifest descriptions never do, so the stripped form is the lowest
// common denominator for description matching. Also unescapes `\ ` → ` ` to
// bridge the activity log's Objective-C-style escaping of embedded spaces in
// SwiftDriver / SwiftEmitModule signatures.
func stripInTargetSuffix(key string) string {
	// Strip the trailing (in target '…' from project '…') clause — one
	// predictable suffix, cheaper to find by substring than regex.
	if idx := strings.Index(key, " (in target '"); idx > 0 {
		key = key[:idx]
	}

	return strings.ReplaceAll(key, `\ `, " ")
}

// Private — SLF parse

// commandInvocationClass is the one class name whose instances the v0
// analyzer surfaces. Other sections (Preparation, BuildOperation, Target
// dependency graph) are structurally the same SLF layout but carry no
// executed-task payload — their commandDetail is a label, not a cmdline.
const commandInvocationClass = "IDEActivityLogCommandInvocationSection"

// topLevelSectionClass is the enclosing section that wraps everything and
// carries the overall build title + start/stop timestamps.
const topLevelSectionClass = "IDEActivityLogSection"

// parseReport walks the SLF token stream once. Section fields vary in count
// (attachments and nested messages are variable-length lists), so instead of
// consuming a fixed window per class, the walker buffers tokens between
// consecutive section openers (classRef to IDEActivityLogSection or
// IDEActivityLogCommandInvocationSection). Attachments and diagnostics are
// classRef to OTHER classes; those are not section boundaries and get
// swallowed into the current buffer, where the longest string still surfaces
// as commandDetail. A later schema-driven walker is v1 scope.
func parseReport(body []byte) (Report, error) {
	rd, err := newSLFReader(body)
	if err != nil {
		return Report{}, err
	}

	st := &parseState{pool: []string{""}}

	for !rd.atEOF() {
		tok, err := rd.next()
		if err != nil {
			if errors.Is(err, errEOF) {
				break
			}

			return Report{}, err
		}

		st.consume(tok)
	}
	st.flush()

	return Report{Build: st.meta, Tasks: st.tasks}, nil
}

// parseState is the walker's working set, factored out to keep parseReport
// below gocognit's threshold without sacrificing readability.
type parseState struct {
	pool []string

	tasks    []Task
	meta     BuildMeta
	buildSet bool

	fields    []token
	isCommand bool
	haveOpen  bool
}

func (s *parseState) consume(tok token) {
	switch tok.kind {
	case tokenClassName:
		s.pool = append(s.pool, tok.strVal)
	case tokenClassRef:
		s.consumeClassRef(tok)
	case tokenInt, tokenString, tokenDouble, tokenNil, tokenListHeader, tokenJSONAttachment:
		if s.haveOpen {
			s.fields = append(s.fields, tok)
		}
	}
}

func (s *parseState) consumeClassRef(tok token) {
	idx := int(tok.intVal)
	switch {
	case idx <= 0 || idx >= len(s.pool):
		// Nil / out-of-range ref — treat as a plain nil field inside the
		// current section.
		if s.haveOpen {
			s.fields = append(s.fields, token{kind: tokenNil})
		}
	case s.pool[idx] == commandInvocationClass || s.pool[idx] == topLevelSectionClass:
		s.flush()
		s.haveOpen = true
		s.isCommand = s.pool[idx] == commandInvocationClass
	default:
		// Attachment or diagnostic — not a section boundary. The field
		// tokens that follow still belong to the enclosing section; we skip
		// the ref itself but keep collecting.
	}
}

func (s *parseState) flush() {
	if !s.haveOpen {
		return
	}

	if s.isCommand {
		if task, ok := taskFromFields(s.fields); ok {
			s.tasks = append(s.tasks, task)
		}
	} else if !s.buildSet {
		if m, ok := metaFromFields(s.fields); ok {
			s.meta = m
			s.buildSet = true
		}
	}

	s.fields = s.fields[:0]
	s.haveOpen = false
	s.isCommand = false
}

// taskFromFields pulls the Task payload out of a buffered field window. The
// window may include tokens from nested attachments; the lift is heuristic
// (first-string→title, second→signature, first-double→started, …) and good
// enough for v0.
func taskFromFields(fields []token) (Task, bool) {
	var (
		strs    []string
		doubles []float64
		ints    []int64
	)

	for _, tok := range fields {
		switch tok.kind {
		case tokenString:
			strs = append(strs, tok.strVal)
		case tokenDouble:
			doubles = append(doubles, tok.floatVal)
		case tokenInt:
			ints = append(ints, tok.intVal)
		case tokenNil, tokenListHeader, tokenClassName, tokenClassRef, tokenJSONAttachment:
			// irrelevant for the lift
		}
	}

	if len(strs) < 2 || len(doubles) < 2 {
		return Task{}, false
	}

	// strs[0] = domainType, strs[1] = title, strs[2] = signature. When the
	// section omits domainType (seen on top-level only), fall back to the
	// earliest string.
	signature := strs[1]
	if len(strs) >= 3 {
		signature = strs[2]
	}

	started := doubles[0]
	stopped := doubles[1]

	var commandDetail string
	for _, s := range strs {
		if len(s) > len(commandDetail) {
			commandDetail = s
		}
	}

	var exitCode int
	if len(ints) > 0 {
		exitCode = int(ints[len(ints)-1])
	}

	return Task{
		Key:           signature,
		Target:        extractTarget(signature),
		Tool:          extractTool(signature),
		StartedAt:     absoluteTimeToTime(started),
		Duration:      durationBetween(started, stopped),
		ExitCode:      exitCode,
		StdoutExcerpt: "",
		StderrExcerpt: "",
		Errors:        extractErrors(commandDetail),
		Ran:           true,
	}, true
}

// metaFromFields pulls the BuildMeta payload out of the top-level
// IDEActivityLogSection — identical shape as taskFromFields but only the
// title + bracketing timestamps matter.
func metaFromFields(fields []token) (BuildMeta, bool) {
	var (
		strs    []string
		doubles []float64
	)

	for _, tok := range fields {
		switch tok.kind {
		case tokenString:
			strs = append(strs, tok.strVal)
		case tokenDouble:
			doubles = append(doubles, tok.floatVal)
		case tokenInt, tokenNil, tokenListHeader, tokenClassName, tokenClassRef, tokenJSONAttachment:
			// irrelevant for the lift
		}
	}

	if len(strs) < 2 || len(doubles) < 2 {
		return BuildMeta{}, false
	}

	return BuildMeta{
		Title:      strs[1],
		StartedAt:  absoluteTimeToTime(doubles[0]),
		FinishedAt: absoluteTimeToTime(doubles[1]),
		Duration:   durationBetween(doubles[0], doubles[1]),
	}, true
}

// Private — field helpers

// targetFromSignatureRegexp pulls the target name out of signatures shaped
// like `Ld /path/Foo.app/Foo normal arm64 (in target 'Foo' from project 'Foo')`.
//
//nolint:gochecknoglobals // compiled once, read-only
var targetFromSignatureRegexp = regexp.MustCompile(`in target '([^']+)'`)

func extractTarget(signature string) string {
	if m := targetFromSignatureRegexp.FindStringSubmatch(signature); m != nil {
		return m[1]
	}

	return ""
}

// extractTool returns the first whitespace-delimited token of the signature.
// For cache-aware signatures (`Ld /path normal arm64 ...`) this is the tool
// label llbuild uses. Compute/discovery sections return the whole short
// label, which is still useful as a tool hint.
func extractTool(signature string) string {
	sig := strings.TrimSpace(signature)
	if sig == "" {
		return ""
	}

	if i := strings.IndexAny(sig, " \t"); i > 0 {
		return sig[:i]
	}

	return sig
}

// extractErrors pulls `error:` lines out of a commandDetail blob. Xcode
// embeds compiler stderr verbatim in the emittedOutputText field, which v0
// collapses into commandDetail via the longest-string heuristic.
func extractErrors(blob string) []string {
	if blob == "" {
		return nil
	}

	var out []string
	for _, line := range strings.Split(blob, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "error:") {
			out = append(out, line)
		}
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

// absoluteTimeToTime converts a CoreFoundation absolute time (seconds since
// 2001-01-01 00:00:00 UTC) to time.Time. Xcode writes timeStartedRecording
// and timeStoppedRecording in this reference.
func absoluteTimeToTime(abs float64) time.Time {
	if abs == 0 || math.IsNaN(abs) {
		return time.Time{}
	}

	const cfAbsoluteReferenceDate = 978307200 // 2001-01-01 UTC as unix seconds

	seconds := int64(abs)
	nanos := int64((abs - float64(seconds)) * float64(time.Second))

	return time.Unix(cfAbsoluteReferenceDate+seconds, nanos).UTC()
}

func durationBetween(start, stop float64) time.Duration {
	if start == 0 || stop == 0 || stop < start {
		return 0
	}

	return time.Duration((stop - start) * float64(time.Second))
}
