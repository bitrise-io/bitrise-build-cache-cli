package slf

// ClassIDEActivityLogCommandInvocationSection is the className Xcode emits
// for every leaf command (compile / link / codesign / copy / script-run).
const ClassIDEActivityLogCommandInvocationSection = "IDEActivityLogCommandInvocationSection"

// DetailStepType return values. Public so collectors can switch on them.
const (
	DetailSwiftCompilation           = "swiftCompilation"
	DetailSwiftAggregatedCompilation = "swiftAggregatedCompilation"
	DetailCCompilation               = "cCompilation"
	DetailLinker                     = "linker"
	DetailMergeSwiftModule           = "mergeSwiftModule"
	DetailCopyResourceFile           = "copyResourceFile"
	DetailCopySwiftLibs              = "copySwiftLibs"
	DetailCompileAssetsCatalog       = "compileAssetsCatalog"
	DetailCompileStoryboard          = "compileStoryboard"
	DetailOther                      = "other"
)

// SectionType mirrors Xcode's IDEActivityLogSection.sectionType enum.
// Only three values are documented and in practice we see 0/1/2.
type SectionType int

// BuildStep is the public tree node — one per IDEActivityLogSection decoded
// from the stream. Collectors walk the tree, project out the information
// they care about, and discard it; we deliberately keep this close to
// XCLogParser's BuildStep shape so a future JSON reporter can mirror theirs
// without re-walking Apple's private class layouts.
type BuildStep struct {
	SectionType          int
	DomainType           string
	Title                string
	Signature            string
	TimeStartedRecording float64
	TimeStoppedRecording float64

	SubSteps []*BuildStep

	Text                  string
	Messages              []Message
	WasCancelled          bool
	IsQuiet               bool
	WasFetchedFromCache   bool
	Subtitle              string
	Location              DocumentLocation
	CommandDetailDesc     string
	UniqueIdentifier      string
	LocalizedResultString string
	XcbuildSignature      string
	Attachments           []Attachment

	// ClassName of the originating IDEFoundation class — one of
	// IDEActivityLogSection, IDEActivityLogMajorGroupSection,
	// IDEActivityLogCommandInvocationSection, IDEActivityLogUnitTestSection,
	// DBGConsoleLog, etc.
	ClassName string
}

// Message is a Warning / Error / Note / diagnostic emitted during a step.
type Message struct {
	ClassName               string
	Title                   string
	ShortTitle              string
	TimeEmitted             uint64
	RangeStartInSectionText uint64
	RangeEndInSectionText   uint64
	SubMessages             []Message
	Severity                int
	Type                    string
	Location                DocumentLocation
	CategoryIdent           string
	SecondaryLocations      []DocumentLocation
	AdditionalDescription   string
}

// DocumentLocation covers DVTDocumentLocation + the DVTText / DVTMember /
// IBDocumentMember subclasses. Fields not populated by a given subclass stay
// zero.
type DocumentLocation struct {
	ClassName            string
	DocumentURLString    string
	Timestamp            float64
	StartingLineNumber   uint64
	StartingColumnNumber uint64
	EndingLineNumber     uint64
	EndingColumnNumber   uint64
	CharacterRangeEnd    uint64
	CharacterRangeStart  uint64
	LocationEncoding     uint64
	Member               string
}

// Duration in seconds.
func (b BuildStep) Duration() float64 {
	if b.TimeStoppedRecording <= b.TimeStartedRecording {
		return 0
	}

	return b.TimeStoppedRecording - b.TimeStartedRecording
}

// WarningCount sums the warnings across this step and its sub-steps.
// XCLogParser derives this from Message.Severity on the IDEDiagnostic path.
func (b BuildStep) WarningCount() int {
	return b.countMessagesBySeverity(1)
}

// ErrorCount sums errors across this step and its sub-steps.
func (b BuildStep) ErrorCount() int {
	return b.countMessagesBySeverity(2)
}

func (b BuildStep) countMessagesBySeverity(sev int) int {
	n := 0

	for _, m := range b.Messages {
		if m.Severity == sev {
			n++
		}
	}

	for _, s := range b.SubSteps {
		n += s.countMessagesBySeverity(sev)
	}

	return n
}

// DetailStepType classifies the step via its signature prefix. The first
// branch mirrors XCLogParser's DetailStepType enum (Xcode 10..15 era). The
// second branch covers Xcode 26.x which renamed many command types
// (`CompileSwift ` → `SwiftCompile `, `MergeSwiftModule ` → `SwiftMergeGeneratedHeaders `,
// etc.) — surfaced here under the same output tokens so a downstream
// histogram does not fork per Xcode major.
func (b BuildStep) DetailStepType() string {
	switch {
	// XCLogParser classic shapes.
	case hasPrefix(b.Signature, "CompileC "):
		return "cCompilation"
	case hasPrefix(b.Signature, "CompileSwift "):
		return DetailSwiftCompilation
	case hasPrefix(b.Signature, "CompileSwiftSources "):
		return "swiftAggregatedCompilation"
	case hasPrefix(b.Signature, "Ld "):
		return "linker"
	case hasPrefix(b.Signature, "PhaseScriptExecution "):
		return "scriptExecution"
	case hasPrefix(b.Signature, "Libtool "):
		return "createStaticLibrary"
	case hasPrefix(b.Signature, "CopySwiftLibs "):
		return "copySwiftLibs"
	case hasPrefix(b.Signature, "CompileAssetCatalog"):
		return "compileAssetsCatalog"
	case hasPrefix(b.Signature, "CompileStoryboard "):
		return "compileStoryboard"
	case hasPrefix(b.Signature, "WriteAuxiliaryFile "):
		return "writeAuxiliaryFile"
	case hasPrefix(b.Signature, "LinkStoryboards "):
		return "linkStoryboards"
	case hasPrefix(b.Signature, "CpResource "):
		return "copyResourceFile"
	case hasPrefix(b.Signature, "MergeSwiftModule "):
		return "mergeSwiftModule"
	case hasPrefix(b.Signature, "CompileXIB "):
		return "XIBCompilation"
	case hasPrefix(b.Signature, "PrecompileSwiftBridgingHeader "):
		return "precompileBridgingHeader"
	case hasPrefix(b.Signature, "ValidateEmbeddedBinary "):
		return "validateEmbeddedBinary"
	case hasPrefix(b.Signature, "Validate "):
		return "validate"

	// Xcode 26.x command name shapes.
	case hasPrefix(b.Signature, "SwiftCompile "):
		return DetailSwiftCompilation
	case hasPrefix(b.Signature, "SwiftDriver "), hasPrefix(b.Signature, "SwiftDriverJobDiscovery "), hasPrefix(b.Signature, "SwiftDriver\\"):
		return "swiftAggregatedCompilation"
	case hasPrefix(b.Signature, "SwiftEmitModule "):
		return DetailSwiftCompilation
	case hasPrefix(b.Signature, "SwiftMergeGeneratedHeaders "):
		return "mergeSwiftModule"
	case hasPrefix(b.Signature, "CodeSign "):
		return "codeSign"
	case hasPrefix(b.Signature, "Copy "):
		return "copyResourceFile"
	case hasPrefix(b.Signature, "ProcessInfoPlistFile "):
		return "processInfoPlist"
	case hasPrefix(b.Signature, "RegisterExecutionPolicyException "):
		return "registerExecutionPolicy"
	}

	return "other"
}

// IsTargetStep flags steps whose domain is a target (one level below the
// main step in a typical build log).
func (b BuildStep) IsTargetStep() bool {
	return hasPrefix(b.DomainType, "Xcode.IDEActivityLogDomainType.target")
}

// IsMainStep flags the top-level build/clean/test/archive step.
func (b BuildStep) IsMainStep() bool {
	return b.DomainType == "Xcode.IDEActivityLogDomainType.BuildLog"
}

func hasPrefix(s, p string) bool {
	return len(s) >= len(p) && s[:len(p)] == p
}
