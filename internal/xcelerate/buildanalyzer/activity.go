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
//     stamps Planned=true + overrides Tool + fills Inputs/Outputs/Args.
//   - appends a synthetic Task (Ran=false, Planned=true) for every Command
//     that had no activity-log section.
//   - derives Dependency edges from the manifest's input/output file-path
//     join: when Command A outputs file F and Command B inputs F, emit
//     Dependency{From: A.Key, To: B.Key} — using the priority-stripped key
//     on both ends so the edge strings read cleanly.
//
// Commands with no description are orchestration artifacts (phony,
// stale-file-removal, create-build-directory …) that never produce an
// activity-log section; they're skipped entirely so the DAG stays readable.
func JoinManifest(report *Report, manifest Manifest) {
	// Index Commands by description + signature. Each commandEntry carries
	// BOTH the stripped key (used for Report-side identity + edges) and the
	// original manifestKey (used for the matched-set bookkeeping), so a
	// re-run on the same report stays idempotent and dependency emission
	// uses the clean string.

	bySignature := make(map[string]commandEntry, len(manifest.Commands))
	byDescription := make(map[string]commandEntry, len(manifest.Commands))
	// swiftDriverCompilations: target name -> entry for the top-level
	// `SwiftDriver Compilation <target> normal <arch>` command. Activity-log
	// sub-events emitted by the swift driver (SwiftCompile per-file,
	// SwiftEmitModule, SwiftDriverJobDiscovery) have no separate manifest
	// entry but belong to this parent — we attribute them so the TOP-N
	// slowest list isn't dominated by unjoined "ran but not planned" rows.
	swiftDriverCompilations := make(map[string]commandEntry)

	for key, cmd := range manifest.Commands {
		if cmd.Description == "" {
			continue
		}

		stripped, priority := stripPriorityPrefix(key)
		entry := commandEntry{
			strippedKey: stripped,
			manifestKey: key,
			priority:    priority,
			cmd:         cmd,
		}
		if cmd.Signature != "" {
			bySignature[cmd.Signature] = entry
		}
		byDescription[normalizeMatchKey(cmd.Description)] = entry

		if target := extractSwiftDriverCompilationTarget(cmd.Description); target != "" {
			swiftDriverCompilations[target] = entry
		}
	}

	matched := make(map[string]struct{}, len(manifest.Commands))

	for i := range report.Tasks {
		task := &report.Tasks[i]

		hit, ok := matchCommand(task.Key, bySignature, byDescription, swiftDriverCompilations)
		if !ok {
			continue
		}

		matched[hit.manifestKey] = struct{}{}

		task.Planned = true
		task.ManifestKey = hit.manifestKey
		task.Priority = hit.priority
		// Override v0's first-word tool heuristic with the manifest's llbuild
		// tool label — `linker`, `swift-driver-compilation`, `shell`, etc.
		// The manifest value is canonical; v0's extractTool is a prefix hack.
		task.Tool = hit.cmd.Tool
		task.Inputs = hit.cmd.Inputs
		task.Outputs = hit.cmd.Outputs
		task.Args = hit.cmd.Args
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
		stripped, priority := stripPriorityPrefix(key)
		report.Tasks = append(report.Tasks, Task{
			Key:         stripped,
			ManifestKey: key,
			Priority:    priority,
			Tool:        cmd.Tool,
			Target:      extractTarget(cmd.Description),
			Planned:     true,
			Ran:         false,
			Inputs:      cmd.Inputs,
			Outputs:     cmd.Outputs,
			Args:        cmd.Args,
		})
	}

	report.Dependencies = deriveDependencies(manifest)
}

// matchCommand runs a Task.Key through the three lookup indexes in priority
// order: exact signature match first (the strongest identity), then
// normalized description (handles the `\ ` escape differences between
// activity-log signatures and manifest descriptions), then the swift-driver
// sub-event fallback that attributes SwiftCompile / SwiftEmitModule /
// SwiftDriverJobDiscovery activity-log rows to their parent manifest
// `SwiftDriver Compilation <target>` command.
func matchCommand(taskKey string, bySignature, byDescription, swiftDrivers map[string]commandEntry) (commandEntry, bool) {
	if entry, found := bySignature[taskKey]; found {
		return entry, true
	}

	normalized := normalizeMatchKey(stripInTargetSuffix(taskKey))
	if entry, found := byDescription[normalized]; found {
		return entry, true
	}

	if target := extractSwiftSubEventTarget(taskKey); target != "" {
		if entry, found := swiftDrivers[target]; found {
			return entry, true
		}
	}

	return commandEntry{}, false
}

// commandEntry mirrors JoinManifest's inner type; exported at package scope
// so matchCommand can take it as a parameter.
type commandEntry struct {
	strippedKey string
	manifestKey string
	priority    int
	cmd         ManifestCommand
}

// deriveDependencies walks the manifest commands and emits one edge per
// input/output file-path join. Orchestration commands (empty description) are
// skipped on both sides so the DAG surfaces real tasks only. Edge endpoints
// carry the priority-stripped key form to match Task.Key.
func deriveDependencies(manifest Manifest) []Dependency {
	// Index: file path -> list of stripped command keys that produce it.
	producers := make(map[string][]string, len(manifest.Commands))

	for key, cmd := range manifest.Commands {
		if cmd.Description == "" {
			continue
		}

		stripped, _ := stripPriorityPrefix(key)
		for _, out := range cmd.Outputs {
			producers[out] = append(producers[out], stripped)
		}
	}

	seen := make(map[string]struct{}, len(manifest.Commands))
	deps := make([]Dependency, 0, len(manifest.Commands))

	for consumerKey, cmd := range manifest.Commands {
		if cmd.Description == "" {
			continue
		}

		strippedConsumer, _ := stripPriorityPrefix(consumerKey)
		for _, in := range cmd.Inputs {
			for _, producerKey := range producers[in] {
				if producerKey == strippedConsumer {
					continue
				}

				edgeID := producerKey + "\x00" + strippedConsumer
				if _, dup := seen[edgeID]; dup {
					continue
				}
				seen[edgeID] = struct{}{}

				deps = append(deps, Dependency{From: producerKey, To: strippedConsumer})
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
// common denominator for description matching.
func stripInTargetSuffix(key string) string {
	// Strip the trailing (in target '…' from project '…') clause — one
	// predictable suffix, cheaper to find by substring than regex.
	if idx := strings.Index(key, " (in target '"); idx > 0 {
		key = key[:idx]
	}

	return key
}

// normalizeMatchKey normalizes a string so activity-log signatures and
// manifest descriptions compare equal regardless of Xcode's whitespace
// encoding. Applied to BOTH sides of the join. Current fold-ins, observed on
// Xcode 26.x MinimalApp:
//   - `\ ` (backslash-space) -> ` `. Activity-log signatures escape embedded
//     spaces in SwiftDriver / SwiftEmitModule entries; manifest descriptions
//     don't.
//   - runs of ASCII whitespace -> a single space. Keeps the match tolerant
//     to future drift (e.g. tabs inserted by a tool version bump).
//   - leading/trailing whitespace trimmed.
//
// The normalization is intentionally cheap — one pass, no regex compile —
// because JoinManifest calls it per-command and per-task.
func normalizeMatchKey(s string) string {
	s = strings.ReplaceAll(s, `\ `, " ")

	var b strings.Builder
	b.Grow(len(s))

	lastSpace := false
	for i := range len(s) {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if !lastSpace && b.Len() > 0 {
				b.WriteByte(' ')
			}
			lastSpace = true

			continue
		}
		lastSpace = false
		b.WriteByte(c)
	}

	out := b.String()
	if len(out) > 0 && out[len(out)-1] == ' ' {
		out = out[:len(out)-1]
	}

	return out
}

// stripPriorityPrefix peels the llbuild `P<N>:<scope>:<config>:` prefix off
// a manifest command key, returning the bare description tail plus the
// numeric priority. The prefix encodes two fields after the priority:
//
//   - Top-level commands have both empty: `P0:::<desc>` — three consecutive
//     colons after `P<N>`.
//   - Target-scoped commands carry `target-<guid>:<config>`:
//     `P2:target-X:Debug:<desc>` — two colons, non-empty fields in between.
//
// So the strip rule is uniform: drop exactly two more colon-terminated
// fields after the priority colon. The returned priority is 0 when the
// prefix is missing or malformed — virtual nodes like `<all>` and
// `<target-…>` fall into that bucket, and the whole key passes through.
func stripPriorityPrefix(key string) (string, int) {
	if len(key) < 2 || key[0] != 'P' {
		return key, 0
	}

	// Scan digits after 'P'.
	i := 1
	for i < len(key) && key[i] >= '0' && key[i] <= '9' {
		i++
	}
	if i == 1 || i >= len(key) || key[i] != ':' {
		return key, 0
	}

	priority := 0
	for j := 1; j < i; j++ {
		priority = priority*10 + int(key[j]-'0')
	}

	// Walk past the two scope fields: find the next two colons.
	rest := key[i+1:]
	for range 2 {
		colon := strings.IndexByte(rest, ':')
		if colon < 0 {
			return key, 0
		}
		rest = rest[colon+1:]
	}

	return rest, priority
}

// extractSwiftDriverCompilationTarget returns the target name encoded in a
// manifest description of the form `SwiftDriver Compilation <target>
// normal <arch> com.apple.xcode.tools.swift.compiler`. Empty when the
// description is not a swift-driver compilation entry.
//
// Used by JoinManifest to build a target-indexed table of swift-driver
// commands, so the activity log's sub-event rows can attach back to their
// parent.
func extractSwiftDriverCompilationTarget(description string) string {
	const prefix = "SwiftDriver Compilation "
	if !strings.HasPrefix(description, prefix) {
		return ""
	}

	rest := description[len(prefix):]

	// Skip an optional "Requirements " token — SwiftDriver also emits a
	// `SwiftDriver Compilation Requirements <target> ...` command whose
	// activity-log sub-events we do NOT want to attribute to the main
	// compilation (they have their own driver entry already matched by
	// description).
	if strings.HasPrefix(rest, "Requirements ") {
		return ""
	}

	// `<target> normal <arch> ...` — target is the first whitespace-delimited
	// token.
	if idx := strings.IndexByte(rest, ' '); idx > 0 {
		return rest[:idx]
	}

	return rest
}

// extractSwiftSubEventTarget returns the target name encoded in an
// activity-log task key for a swift-driver sub-event (SwiftCompile,
// SwiftEmitModule, SwiftDriverJobDiscovery). These rows have no separate
// manifest entry — the swift driver emits them internally — so the join
// attributes them to the parent `SwiftDriver Compilation <target> ...`
// command via the target name lifted from the `in target '…'` suffix.
// Empty when the key is not a swift sub-event or omits the suffix.
func extractSwiftSubEventTarget(taskKey string) string {
	switch {
	case strings.HasPrefix(taskKey, "SwiftCompile "),
		strings.HasPrefix(taskKey, "SwiftEmitModule "),
		strings.HasPrefix(taskKey, "SwiftDriverJobDiscovery "):
		return extractTarget(taskKey)
	default:
		return ""
	}
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
