package enrichment

import (
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/sessions"
)

// RowType is the enricher's single-ID classification of a sidecar by its
// build-request (action, qos) tuple. Every sidecar lands in exactly one row
// type at parse time; downstream grouping (DarkBuild → MainBuild merge) is
// driven by the type pair.
type RowType string

const (
	RowMainBuild      RowType = "main_build"
	RowDarkBuild      RowType = "dark_build"
	RowIndexer        RowType = "indexer"
	RowPackageResolve RowType = "package_resolve"
	RowSigning        RowType = "signing"
	RowLocalization   RowType = "localization"
	RowClean          RowType = "clean"
	RowArchive        RowType = "archive"
	RowTest           RowType = "test"
	RowInstallHeaders RowType = "install_headers"
	RowUnclassified   RowType = "unclassified"
)

// BuildRequestAction constants mirror the strings Xcode writes into
// build-request.json's action field. Exported so analytics / telemetry can
// reference the same table.
const (
	BuildRequestActionBuild            = "build"
	BuildRequestActionIndexBuild       = "indexbuild"
	BuildRequestActionResolvePackages  = "resolvePackages"
	BuildRequestActionResolve          = "resolve"
	BuildRequestActionCleanBuildFolder = "cleanBuildFolder"
	BuildRequestActionClean            = "clean"
	BuildRequestActionArchive          = "archive"
	BuildRequestActionTest             = "test"
	BuildRequestActionInstallHeaders   = "installHeaders"

	BuildRequestQoSDefault = "default"
)

// Classify maps a sidecar's (action, qos) pair to its RowType per the IDE
// ruleset table pinned in docs/project_wrapperless_cas_pipeline_and_ruleset:
//
//	build        +  <absent>              → MainBuild
//	build        +  default               → DarkBuild
//	indexbuild   +  prepareForIndexing    → Indexer
//	resolvePackages  / resolve            → PackageResolve
//	cleanBuildFolder / clean              → Clean
//	archive                               → Archive
//	test                                  → Test
//	installHeaders                        → InstallHeaders
//	anything else                         → Unclassified
//
// Signing and Localization have no action string in build-request.json today;
// they land as Unclassified and are picked up via plist boundary in the
// orphan sweep until a dedicated signal exists.
func Classify(action, qos string) RowType {
	switch action {
	case BuildRequestActionBuild:
		if qos == BuildRequestQoSDefault {
			return RowDarkBuild
		}

		return RowMainBuild
	case BuildRequestActionIndexBuild:
		return RowIndexer
	case BuildRequestActionResolvePackages, BuildRequestActionResolve:
		return RowPackageResolve
	case BuildRequestActionCleanBuildFolder, BuildRequestActionClean:
		return RowClean
	case BuildRequestActionArchive:
		return RowArchive
	case BuildRequestActionTest:
		return RowTest
	case BuildRequestActionInstallHeaders:
		return RowInstallHeaders
	default:
		return RowUnclassified
	}
}

// GroupKey returns the proxy-derived InvocationID the sidecar's PUT must be
// keyed under. This is the single-ID pipeline's anchor: plist UUIDs and
// boundary UUIDs become payload metadata, never row keys. Returns empty for
// sidecars whose anchor failed (health probe peers, wrapper-owned conns).
func GroupKey(s sessions.Sidecar) string {
	if s.WrapperSession == nil {
		return ""
	}

	return s.WrapperSession.InvocationID
}
