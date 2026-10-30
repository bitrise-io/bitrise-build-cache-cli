package slf

import (
	"errors"
	"fmt"
	"io"
)

// Decode is the top-level entry: gzip + lex + build the public BuildStep
// tree. On unknown classes we fail-open — the decoder records an
// `UnknownClasses` list on the returned *BuildStep.Err rather than aborting
// so forward Xcode-majors keep producing something useful.
func Decode(r io.Reader) (*BuildStep, uint64, error) {
	version, tokens, err := Tokenize(r)
	if err != nil {
		return nil, 0, err
	}

	root, err := decodeTokens(tokens, version)
	if err != nil {
		return nil, version, err
	}

	return root, version, nil
}

// DecodeBytes is Decode on an already-decompressed body.
func DecodeBytes(body []byte) (*BuildStep, uint64, error) {
	version, tokens, err := TokenizeBytes(body)
	if err != nil {
		return nil, 0, err
	}

	root, err := decodeTokens(tokens, version)
	if err != nil {
		return nil, version, err
	}

	return root, version, nil
}

type decoder struct {
	tokens  []Token
	pos     int
	version uint64

	// isCommandLineLog mirrors XCLogParser's probe: the top-level section
	// may be preceded by an extra int that re-appears as a trailing "unknown"
	// field. Set by parseSection on the very first section only.
	isCommandLineLog bool
}

func decodeTokens(tokens []Token, version uint64) (*BuildStep, error) {
	d := &decoder{tokens: tokens, version: version}

	root, err := d.parseSection()
	if err != nil {
		return nil, fmt.Errorf("slf decode: %w", err)
	}

	return root, nil
}

// next returns the next token or an EOF error.
func (d *decoder) next() (Token, error) {
	if d.pos >= len(d.tokens) {
		return Token{}, io.ErrUnexpectedEOF
	}

	t := d.tokens[d.pos]
	d.pos++

	return t, nil
}

func (d *decoder) peek() (Token, bool) {
	if d.pos >= len(d.tokens) {
		return Token{}, false
	}

	return d.tokens[d.pos], true
}

func (d *decoder) rewind() {
	if d.pos > 0 {
		d.pos--
	}
}

// classRef consumes an optional className then a required classNameRef (or
// a null — null means "no instance", which document-location fields use).
func (d *decoder) classRef() (string, bool, error) {
	t, err := d.next()
	if err != nil {
		return "", false, err
	}

	if t.Type == TokenNull {
		return "", false, nil
	}

	// First-time encounter: className followed by its matching classNameRef.
	if t.Type == TokenClassName {
		next, err := d.next()
		if err != nil {
			return "", false, err
		}

		if next.Type != TokenClassNameRef {
			return "", false, fmt.Errorf("slf: expected classNameRef after className, got %s", next)
		}

		return next.StringValue, true, nil
	}

	if t.Type == TokenClassNameRef {
		return t.StringValue, true, nil
	}

	return "", false, fmt.Errorf("slf: expected class-ref, got %s", t)
}

func (d *decoder) parseInt() (uint64, error) {
	t, err := d.next()
	if err != nil {
		return 0, err
	}

	if t.Type != TokenInt {
		return 0, fmt.Errorf("slf: expected int, got %s", t)
	}

	return t.IntValue, nil
}

func (d *decoder) parseDouble() (float64, error) {
	t, err := d.next()
	if err != nil {
		return 0, err
	}

	if t.Type != TokenDouble {
		return 0, fmt.Errorf("slf: expected double, got %s", t)
	}

	return t.DoubleValue, nil
}

func (d *decoder) parseString() (string, error) {
	t, err := d.next()
	if err != nil {
		return "", err
	}

	switch t.Type {
	case TokenString:
		return t.StringValue, nil
	case TokenNull:
		return "", nil
	}

	return "", fmt.Errorf("slf: expected string, got %s", t)
}

func (d *decoder) parseBool() (bool, error) {
	v, err := d.parseInt()
	if err != nil {
		return false, err
	}

	return v == 1, nil
}

// parseListHeader reads a list length (or returns 0 for a null list, which
// Xcode uses interchangeably with `(0`).
func (d *decoder) parseListHeader() (int, error) {
	t, err := d.next()
	if err != nil {
		return 0, err
	}

	switch t.Type {
	case TokenNull:
		return 0, nil
	case TokenList:
		return t.ListCount, nil
	}

	return 0, fmt.Errorf("slf: expected list header, got %s", t)
}

// parseSection handles the "section-shaped" classes that extend
// IDEActivityLogSection. We treat IDEActivityLogSection,
// IDEActivityLogMajorGroupSection, IDEActivityLogCommandInvocationSection,
// IDEActivityLogUnitTestSection, IDECommandLineBuildLog and DBGConsoleLog
// as the same base shape; subclasses may read extra fields after the base
// section, which we currently drop (we don't surface test / console content).
func (d *decoder) parseSection() (*BuildStep, error) {
	t, err := d.next()
	if err != nil {
		return nil, err
	}

	// Top-level: an extra int before the first classRef means a
	// command-line (xcodebuild CLI) log; the trailing "unknown" field at the
	// end of the root section is then present.
	if t.Type == TokenInt {
		d.isCommandLineLog = true
		t, err = d.next()
		if err != nil {
			return nil, err
		}
	}

	className, ok, err := d.resolveClassRef(t)
	if err != nil {
		return nil, err
	}

	if !ok {
		return nil, errors.New("slf: expected a section class-ref, got null")
	}

	bs, err := d.parseLogSection(className)
	if err != nil {
		return nil, err
	}

	// Trailing-class-specific fields on known subclasses: skip the extras so
	// the pos cursor stays aligned for the next sibling.
	if err := d.skipSubclassTail(className); err != nil {
		return nil, err
	}

	return bs, nil
}

// resolveClassRef turns a token that is either a className or classNameRef
// (or a leading className followed by its classNameRef) into a string.
func (d *decoder) resolveClassRef(t Token) (string, bool, error) {
	switch t.Type {
	case TokenNull:
		return "", false, nil
	case TokenClassName:
		next, err := d.next()
		if err != nil {
			return "", false, err
		}

		if next.Type != TokenClassNameRef {
			return "", false, fmt.Errorf("slf: expected classNameRef after className, got %s", next)
		}

		return next.StringValue, true, nil
	case TokenClassNameRef:
		return t.StringValue, true, nil
	}

	return "", false, fmt.Errorf("slf: expected class-ref, got %s", t)
}

// parseLogSection reads the common IDEActivityLogSection property tape.
// Property ordering matches XCLogParser's `parseIDEActivityLogSection`.
// Version-aware branches:
//   - v26.2+ (Xcode 26.2 / SLF v12+) inserts an int before `subtitle`
//     — probe on token type.
//   - v27+ (SLF v13+) inserts an int before `text`
//     — same probe strategy.
//   - v11+ reads the attachment list; v10 and earlier skip it.
func (d *decoder) parseLogSection(className string) (*BuildStep, error) {
	bs := &BuildStep{ClassName: className}

	sectionType, err := d.parseInt()
	if err != nil {
		return nil, err
	}

	bs.SectionType = int(sectionType)

	if bs.DomainType, err = d.parseString(); err != nil {
		return nil, err
	}

	if bs.Title, err = d.parseString(); err != nil {
		return nil, err
	}

	if bs.Signature, err = d.parseString(); err != nil {
		return nil, err
	}

	if bs.TimeStartedRecording, err = d.parseDouble(); err != nil {
		return nil, err
	}

	if bs.TimeStoppedRecording, err = d.parseDouble(); err != nil {
		return nil, err
	}

	if bs.SubSteps, err = d.parseSections(); err != nil {
		return nil, err
	}

	// Probe for the v27+ pre-text int.
	if next, ok := d.peek(); ok && next.Type == TokenInt {
		// Consume + discard — Xcode exposes no documented use for it today.
		_, _ = d.next()
	}

	if bs.Text, err = d.parseString(); err != nil {
		return nil, err
	}

	if bs.Messages, err = d.parseMessages(); err != nil {
		return nil, err
	}

	if bs.WasCancelled, err = d.parseBool(); err != nil {
		return nil, err
	}

	if bs.IsQuiet, err = d.parseBool(); err != nil {
		return nil, err
	}

	if bs.WasFetchedFromCache, err = d.parseBool(); err != nil {
		return nil, err
	}

	// Probe for the v26.2+ pre-subtitle int.
	var seenPreSubtitleInt bool
	if next, ok := d.peek(); ok && next.Type == TokenInt {
		_, _ = d.next()

		seenPreSubtitleInt = true
	}

	if bs.Subtitle, err = d.parseString(); err != nil {
		return nil, err
	}

	if bs.Location, err = d.parseDocumentLocation(); err != nil {
		return nil, err
	}

	if bs.CommandDetailDesc, err = d.parseString(); err != nil {
		return nil, err
	}

	if bs.UniqueIdentifier, err = d.parseString(); err != nil {
		return nil, err
	}

	if bs.LocalizedResultString, err = d.parseString(); err != nil {
		return nil, err
	}

	if bs.XcbuildSignature, err = d.parseString(); err != nil {
		return nil, err
	}

	// Attachments appear from SLF v11 onwards.
	if d.version >= 11 {
		if bs.Attachments, err = d.parseAttachments(); err != nil {
			return nil, err
		}
	}

	// Pre-v26.2 CLI logs: the trailing unknown int appears here instead of
	// before subtitle. On v26.2+ we already consumed it above.
	if !seenPreSubtitleInt && d.isCommandLineLog {
		if _, err := d.parseInt(); err != nil {
			return nil, err
		}
	}

	return bs, nil
}

// skipSubclassTail eats the trailing fields that subclasses add after the
// base IDEActivityLogSection block. We don't model them yet; dropping them
// keeps the token cursor aligned for the next sibling.
func (d *decoder) skipSubclassTail(className string) error {
	switch className {
	case "IDEActivityLogUnitTestSection":
		// 6 trailing strings: testsPassedString, durationString, summaryString,
		// suiteName, testName, performanceTestOutputString.
		for range 6 {
			if _, err := d.parseString(); err != nil {
				return err
			}
		}
	case "DBGConsoleLog":
		// Trailing: IDEConsoleItem list.
		if err := d.skipConsoleItems(); err != nil {
			return err
		}
	}

	return nil
}

func (d *decoder) skipConsoleItems() error {
	n, err := d.parseListHeader()
	if err != nil {
		return err
	}

	for range n {
		cn, ok, err := d.classRef()
		if err != nil {
			return err
		}

		if !ok {
			continue
		}

		if cn != "IDEConsoleItem" {
			return fmt.Errorf("slf: unexpected console item class %q", cn)
		}

		// adaptorType uint, content string, kind uint, timestamp double.
		if _, err := d.parseInt(); err != nil {
			return err
		}

		if _, err := d.parseString(); err != nil {
			return err
		}

		if _, err := d.parseInt(); err != nil {
			return err
		}

		if _, err := d.parseDouble(); err != nil {
			return err
		}
	}

	return nil
}

// parseSections reads a list of IDEActivityLogSection — either a `(N` list
// header followed by N sections, or a null for empty.
func (d *decoder) parseSections() ([]*BuildStep, error) {
	n, err := d.parseListHeader()
	if err != nil {
		return nil, err
	}

	if n == 0 {
		return nil, nil
	}

	out := make([]*BuildStep, 0, n)

	for i := 0; i < n; i++ {
		s, err := d.parseSection()
		if err != nil {
			return nil, fmt.Errorf("sub-section %d/%d: %w", i, n, err)
		}

		out = append(out, s)
	}

	return out, nil
}

// parseMessages reads a list of IDEActivityLogMessage (or any subclass).
func (d *decoder) parseMessages() ([]Message, error) {
	n, err := d.parseListHeader()
	if err != nil {
		return nil, err
	}

	if n == 0 {
		return nil, nil
	}

	out := make([]Message, 0, n)

	for i := 0; i < n; i++ {
		m, err := d.parseMessage()
		if err != nil {
			return nil, fmt.Errorf("message %d/%d: %w", i, n, err)
		}

		out = append(out, m)
	}

	return out, nil
}

func (d *decoder) parseMessage() (Message, error) {
	className, ok, err := d.classRef()
	if err != nil {
		return Message{}, err
	}

	if !ok {
		return Message{}, errors.New("slf: null message class-ref")
	}

	m := Message{ClassName: className}

	if m.Title, err = d.parseString(); err != nil {
		return Message{}, err
	}

	if m.ShortTitle, err = d.parseString(); err != nil {
		return Message{}, err
	}

	if m.TimeEmitted, err = d.parseInt(); err != nil {
		return Message{}, err
	}

	if m.RangeEndInSectionText, err = d.parseInt(); err != nil {
		return Message{}, err
	}

	if m.RangeStartInSectionText, err = d.parseInt(); err != nil {
		return Message{}, err
	}

	if m.SubMessages, err = d.parseMessages(); err != nil {
		return Message{}, err
	}

	sev, err := d.parseInt()
	if err != nil {
		return Message{}, err
	}

	m.Severity = int(sev)

	if m.Type, err = d.parseString(); err != nil {
		return Message{}, err
	}

	if m.Location, err = d.parseDocumentLocation(); err != nil {
		return Message{}, err
	}

	if m.CategoryIdent, err = d.parseString(); err != nil {
		return Message{}, err
	}

	if m.SecondaryLocations, err = d.parseDocumentLocations(); err != nil {
		return Message{}, err
	}

	if m.AdditionalDescription, err = d.parseString(); err != nil {
		return Message{}, err
	}

	// Subclass tails.
	switch className {
	case "IDEActivityLogAnalyzerResultMessage":
		// resultType string, keyEventIndex int.
		if _, err := d.parseString(); err != nil {
			return Message{}, err
		}

		if _, err := d.parseInt(); err != nil {
			return Message{}, err
		}
	case "IDEActivityLogAnalyzerEventStepMessage":
		if _, err := d.parseInt(); err != nil {
			return Message{}, err
		}

		if _, err := d.parseString(); err != nil {
			return Message{}, err
		}

		if _, err := d.parseInt(); err != nil {
			return Message{}, err
		}
	case "IDEActivityLogAnalyzerControlFlowStepMessage":
		if _, err := d.parseInt(); err != nil {
			return Message{}, err
		}

		if _, err := d.parseDocumentLocation(); err != nil {
			return Message{}, err
		}

		if err := d.skipControlFlowEdges(); err != nil {
			return Message{}, err
		}
	case "IDEActivityLogActionMessage":
		if _, err := d.parseString(); err != nil {
			return Message{}, err
		}
	}

	return m, nil
}

func (d *decoder) skipControlFlowEdges() error {
	n, err := d.parseListHeader()
	if err != nil {
		return err
	}

	for range n {
		cn, ok, err := d.classRef()
		if err != nil {
			return err
		}

		if !ok {
			continue
		}

		if cn != "IDEActivityLogAnalyzerControlFlowStepEdge" {
			return fmt.Errorf("slf: unexpected edge class %q", cn)
		}

		if _, err := d.parseDocumentLocation(); err != nil {
			return err
		}

		if _, err := d.parseDocumentLocation(); err != nil {
			return err
		}
	}

	return nil
}

// parseDocumentLocation reads a DVTDocumentLocation / DVTTextDocumentLocation
// / DVTMemberDocumentLocation / IBDocumentMemberLocation entry, or returns
// a zero DocumentLocation on a null class-ref.
func (d *decoder) parseDocumentLocation() (DocumentLocation, error) {
	className, ok, err := d.classRef()
	if err != nil {
		return DocumentLocation{}, err
	}

	if !ok {
		return DocumentLocation{}, nil
	}

	loc := DocumentLocation{ClassName: className}

	if loc.DocumentURLString, err = d.parseString(); err != nil {
		return DocumentLocation{}, err
	}

	if loc.Timestamp, err = d.parseDouble(); err != nil {
		return DocumentLocation{}, err
	}

	switch className {
	case "DVTTextDocumentLocation":
		if loc.StartingLineNumber, err = d.parseInt(); err != nil {
			return DocumentLocation{}, err
		}

		if loc.StartingColumnNumber, err = d.parseInt(); err != nil {
			return DocumentLocation{}, err
		}

		if loc.EndingLineNumber, err = d.parseInt(); err != nil {
			return DocumentLocation{}, err
		}

		if loc.EndingColumnNumber, err = d.parseInt(); err != nil {
			return DocumentLocation{}, err
		}

		if loc.CharacterRangeEnd, err = d.parseInt(); err != nil {
			return DocumentLocation{}, err
		}

		if loc.CharacterRangeStart, err = d.parseInt(); err != nil {
			return DocumentLocation{}, err
		}

		if loc.LocationEncoding, err = d.parseInt(); err != nil {
			return DocumentLocation{}, err
		}
	case "DVTMemberDocumentLocation":
		if loc.Member, err = d.parseString(); err != nil {
			return DocumentLocation{}, err
		}
	case "IBDocumentMemberLocation":
		// memberIdentifier IBMemberID { memberIdentifier string },
		// attributeSearchLocation IBAttributeSearchLocation? (we skip)
		cn, ok, err := d.classRef()
		if err != nil {
			return DocumentLocation{}, err
		}

		if ok && cn == "IBMemberID" {
			if _, err := d.parseString(); err != nil {
				return DocumentLocation{}, err
			}
		}

		// Expect a null terminator for the search location.
		if t, err := d.next(); err == nil && t.Type != TokenNull {
			d.rewind()
		}
	case "DVTDocumentLocation", "Xcode3ProjectDocumentLocation", "IDELogDocumentLocation":
		// Base class — fields already consumed.
	}

	return loc, nil
}

func (d *decoder) parseDocumentLocations() ([]DocumentLocation, error) {
	n, err := d.parseListHeader()
	if err != nil {
		return nil, err
	}

	if n == 0 {
		return nil, nil
	}

	out := make([]DocumentLocation, 0, n)

	for i := 0; i < n; i++ {
		loc, err := d.parseDocumentLocation()
		if err != nil {
			return nil, err
		}

		out = append(out, loc)
	}

	return out, nil
}

// parseAttachments reads the SLF v11+ attachment list that trails every
// IDEActivityLogSection. Unknown identifiers keep going with Payload only.
func (d *decoder) parseAttachments() ([]Attachment, error) {
	n, err := d.parseListHeader()
	if err != nil {
		return nil, err
	}

	if n == 0 {
		return nil, nil
	}

	out := make([]Attachment, 0, n)

	for i := 0; i < n; i++ {
		att, err := d.parseAttachment()
		if err != nil {
			return nil, fmt.Errorf("attachment %d/%d: %w", i, n, err)
		}

		out = append(out, att)
	}

	return out, nil
}

func (d *decoder) parseAttachment() (Attachment, error) {
	className, ok, err := d.classRef()
	if err != nil {
		return Attachment{}, err
	}

	if !ok {
		return Attachment{}, errors.New("slf: null attachment class-ref")
	}

	if className != "IDEFoundation.IDEActivityLogSectionAttachment" {
		return Attachment{}, fmt.Errorf("slf: unexpected attachment class %q", className)
	}

	identifier, err := d.parseString()
	if err != nil {
		return Attachment{}, err
	}

	major, err := d.parseInt()
	if err != nil {
		return Attachment{}, err
	}

	minor, err := d.parseInt()
	if err != nil {
		return Attachment{}, err
	}

	// Payload is a `*`-JSON token (or occasionally null).
	tok, err := d.next()
	if err != nil {
		return Attachment{}, err
	}

	att := Attachment{Identifier: identifier, MajorVersion: major, MinorVersion: minor}

	switch tok.Type {
	case TokenJSON:
		att.Payload = []byte(tok.StringValue)
	case TokenNull:
		// nothing to decode
	default:
		return Attachment{}, fmt.Errorf("slf: expected JSON attachment payload, got %s", tok)
	}

	decodeAttachment(&att)

	return att, nil
}
