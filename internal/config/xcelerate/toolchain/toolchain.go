// Package toolchain stages a custom Xcode toolchain that redirects the
// dsymutil GenerateDSYMFile dispatch to the CLI's CAS-plugin shim.
//
// Layout under ~/.bitrise-xcelerate/toolchains/com.bitrise.cas-shim.xctoolchain:
//
//	ToolchainInfo.plist, Info.plist   — identifies the toolchain as com.bitrise.cas-shim
//	usr/bin/dsymutil                  — bash trampoline that exec's `bitrise-build-cache-cli xcelerate dsymutil-shim --`
//	usr/bin/<other>                   — symlinks to the stock XcodeDefault.xctoolchain
//	usr/lib, usr/libexec, usr/share   — symlinks to the stock siblings
//	stamp.json                        — xcode-select -p + Xcode build number; drives re-stage on bump
//
// The farm root is symlinked into ~/Library/Developer/Toolchains so xcodebuild
// scans it on the TOOLCHAINS build setting. Fails open: if any sibling symlink
// is missing at runtime the shim exec's stock dsymutil.
package toolchain

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// Stamp captures the Xcode location + build number the farm was staged against.
// A mismatch (user `xcode-select`'d a new Xcode, or Spotlight re-pointed the
// Developer dir) triggers a nuke-and-re-stage on the next activate.
type Stamp struct {
	DeveloperDir     string    `json:"developerDir"`
	XcodeBuildNumber string    `json:"xcodeBuildNumber"`
	StagedAt         time.Time `json:"stagedAt"`
}

// shimTrampolineTemplate: when the toolchain's usr/bin/dsymutil is exec'd by
// SwiftBuild's GenerateDSYMFile, this bash forwards argv to the CLI shim with
// the "--" separator cobra consumes. Keeping it to one process re-exec saves us
// a Go startup compared with symlinking the CLI directly into usr/bin/.
const shimTrampolineTemplate = `#!/bin/bash
exec %s xcelerate dsymutil-shim -- "$@"
`

// Params carries injected deps so tests can stage into a tempdir without
// touching the real filesystem.
type Params struct {
	Paths   paths.Paths
	OsProxy utils.OsProxy
	Logger  log.Logger

	// CLIPath is the absolute path of the on-disk `bitrise-build-cache-cli`
	// binary the shim trampoline execs. Activate already copies the CLI to
	// ~/.bitrise-xcelerate/bin/bitrise-build-cache-cli.
	CLIPath string

	// StockToolchainDir is the path of the XcodeDefault.xctoolchain the farm
	// mirrors. If zero, resolved from DEVELOPER_DIR / xcode-select -p.
	StockToolchainDir string

	// XcodeVersionProbe returns the Xcode build number for stamp drift detection.
	// Zero value uses `xcodebuild -version` via exec.
	XcodeVersionProbe func(ctx context.Context) (string, error)
}

// Require ensures the farm is staged + current. Called from activate xcode.
// Returns the stamp written (or already present) on success. Errors are
// terminal only if the farm cannot be created at all; a stale symlink in
// Apple's user Toolchains dir is best-effort fixed up.
func Require(ctx context.Context, p Params) (Stamp, error) {
	logger := p.Logger
	osProxy := p.OsProxy
	if osProxy == nil {
		osProxy = utils.DefaultOsProxy{}
	}

	developerDir, err := resolveDeveloperDir(ctx, osProxy)
	if err != nil {
		return Stamp{}, fmt.Errorf("resolve DEVELOPER_DIR: %w", err)
	}

	stockToolchain := p.StockToolchainDir
	if stockToolchain == "" {
		stockToolchain = filepath.Join(developerDir, "Toolchains", "XcodeDefault.xctoolchain")
	}

	buildNumber, err := xcodeBuildNumber(ctx, p)
	if err != nil {
		logger.Debugf("xcode build number probe failed, defaulting to 'unknown': %v", err)

		buildNumber = "unknown"
	}

	want := Stamp{
		DeveloperDir:     developerDir,
		XcodeBuildNumber: buildNumber,
		StagedAt:         time.Now().UTC(),
	}

	farmDir := p.Paths.DsymutilCasShimToolchainStagingDir()
	stampPath := p.Paths.DsymutilCasShimToolchainStampFile()

	if currentMatches(osProxy, stampPath, want) {
		logger.Debugf("dsymutil CAS shim toolchain farm already current: %s", farmDir)
		if err := ensureUserToolchainSymlink(osProxy, p.Paths); err != nil {
			logger.Warnf("Failed to ensure user Toolchains symlink: %v", err)
		}

		return want, nil
	}

	if err := nukeFarm(osProxy, farmDir); err != nil {
		return Stamp{}, fmt.Errorf("nuke stale toolchain farm: %w", err)
	}

	if err := stageFarm(p, farmDir, stockToolchain); err != nil {
		return Stamp{}, fmt.Errorf("stage toolchain farm: %w", err)
	}

	if err := writeStamp(osProxy, stampPath, want); err != nil {
		return Stamp{}, fmt.Errorf("write stamp: %w", err)
	}

	if err := ensureUserToolchainSymlink(osProxy, p.Paths); err != nil {
		return Stamp{}, fmt.Errorf("register toolchain under %s: %w", p.Paths.XcodeUserToolchainsDir(), err)
	}

	return want, nil
}

// Remove deletes the staged farm and the user Toolchains symlink.
// Idempotent; safe to call when nothing is staged.
func Remove(osProxy utils.OsProxy, p paths.Paths) error {
	if osProxy == nil {
		osProxy = utils.DefaultOsProxy{}
	}

	symlink := p.DsymutilCasShimUserToolchainSymlink()
	if err := osProxy.Remove(symlink); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove user Toolchains symlink: %w", err)
	}

	// The staging dir lives under ~/.bitrise-xcelerate which Deactivate already
	// nukes, but call it out explicitly so callers can run just this cleanup.
	farmDir := p.DsymutilCasShimToolchainStagingDir()
	if err := os.RemoveAll(farmDir); err != nil {
		return fmt.Errorf("remove toolchain farm: %w", err)
	}

	return nil
}

// Private — staging

func resolveDeveloperDir(ctx context.Context, _ utils.OsProxy) (string, error) {
	if v := os.Getenv("DEVELOPER_DIR"); v != "" {
		return v, nil
	}

	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	out, err := exec.CommandContext(cctx, "xcode-select", "-p").Output()
	if err != nil {
		return "", fmt.Errorf("xcode-select -p: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

func xcodeBuildNumber(ctx context.Context, p Params) (string, error) {
	if p.XcodeVersionProbe != nil {
		return p.XcodeVersionProbe(ctx)
	}

	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(cctx, "xcodebuild", "-version").Output()
	if err != nil {
		return "", fmt.Errorf("xcodebuild -version: %w", err)
	}

	// Output format:
	//   Xcode 27.0
	//   Build version 27A266a
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "Build version "); ok {
			return after, nil
		}
	}

	return strings.TrimSpace(string(out)), nil
}

func currentMatches(osProxy utils.OsProxy, stampPath string, want Stamp) bool {
	data, found, err := osProxy.ReadFileIfExists(stampPath)
	if err != nil || !found {
		return false
	}

	var got Stamp
	if err := json.Unmarshal([]byte(data), &got); err != nil {
		return false
	}

	return got.DeveloperDir == want.DeveloperDir && got.XcodeBuildNumber == want.XcodeBuildNumber
}

func nukeFarm(_ utils.OsProxy, farmDir string) error {
	if err := os.RemoveAll(farmDir); err != nil {
		return fmt.Errorf("remove %s: %w", farmDir, err)
	}

	return nil
}

func stageFarm(p Params, farmDir, stockToolchain string) error {
	binDir := filepath.Join(farmDir, "usr", "bin")
	if err := p.OsProxy.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", binDir, err)
	}

	if err := writeTrampoline(p, binDir); err != nil {
		return err
	}

	if err := mirrorToolchainContents(farmDir, stockToolchain, binDir); err != nil {
		return err
	}

	if err := writeToolchainPlists(p, farmDir); err != nil {
		return err
	}

	return nil
}

func writeTrampoline(p Params, binDir string) error {
	trampoline := []byte(fmt.Sprintf(shimTrampolineTemplate, p.CLIPath))
	path := filepath.Join(binDir, "dsymutil")
	if err := p.OsProxy.WriteFile(path, trampoline, 0o755); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}

func mirrorToolchainContents(farmDir, stockToolchain, farmBinDir string) error {
	// Symlink every stock top-level dir / file into the farm, except usr (which
	// we partially materialise below).
	entries, err := os.ReadDir(stockToolchain)
	if err != nil {
		return fmt.Errorf("read %s: %w", stockToolchain, err)
	}

	for _, e := range entries {
		if e.Name() == "usr" {
			continue
		}

		src := filepath.Join(stockToolchain, e.Name())
		dst := filepath.Join(farmDir, e.Name())
		if err := os.Symlink(src, dst); err != nil {
			return fmt.Errorf("symlink %s -> %s: %w", dst, src, err)
		}
	}

	// Mirror usr/{lib,libexec,share,include,local} by symlink so the shim's
	// stock-dsymutil fall-through has every sibling tool available.
	usrSrc := filepath.Join(stockToolchain, "usr")
	usrDst := filepath.Join(farmDir, "usr")
	usrEntries, err := os.ReadDir(usrSrc)
	if err != nil {
		return fmt.Errorf("read %s: %w", usrSrc, err)
	}

	for _, e := range usrEntries {
		if e.Name() == "bin" {
			continue
		}

		src := filepath.Join(usrSrc, e.Name())
		dst := filepath.Join(usrDst, e.Name())
		if err := os.Symlink(src, dst); err != nil {
			return fmt.Errorf("symlink %s -> %s: %w", dst, src, err)
		}
	}

	// Mirror every stock usr/bin entry EXCEPT dsymutil (the trampoline wrote dsymutil).
	stockBin := filepath.Join(usrSrc, "bin")
	binEntries, err := os.ReadDir(stockBin)
	if err != nil {
		return fmt.Errorf("read %s: %w", stockBin, err)
	}

	for _, e := range binEntries {
		if e.Name() == "dsymutil" {
			continue
		}

		src := filepath.Join(stockBin, e.Name())
		dst := filepath.Join(farmBinDir, e.Name())
		if err := os.Symlink(src, dst); err != nil {
			return fmt.Errorf("symlink %s -> %s: %w", dst, src, err)
		}
	}

	return nil
}

func writeToolchainPlists(p Params, farmDir string) error {
	infoPlist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleIdentifier</key>
    <string>` + paths.DsymutilCasShimToolchainID + `</string>
    <key>ShortDisplayName</key>
    <string>Bitrise CAS Shim</string>
    <key>CFBundleVersion</key>
    <string>1.0</string>
    <key>DisplayName</key>
    <string>Bitrise Build Cache dsymutil CAS shim</string>
</dict>
</plist>
`
	// Both Info.plist and ToolchainInfo.plist are read by xcodebuild for toolchain discovery.
	for _, name := range []string{"Info.plist", "ToolchainInfo.plist"} {
		path := filepath.Join(farmDir, name)
		// ToolchainInfo.plist exists in the stock toolchain as a symlink target; our
		// farm's symlink-mirror step won't have overwritten the Info.plist location
		// because it skipped `usr` only. Remove any leftover symlink to prevent
		// writing through to the stock toolchain.
		_ = os.Remove(path)
		if err := p.OsProxy.WriteFile(path, []byte(infoPlist), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}

	return nil
}

func writeStamp(osProxy utils.OsProxy, stampPath string, s Stamp) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal stamp: %w", err)
	}

	if err := osProxy.WriteFile(stampPath, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", stampPath, err)
	}

	return nil
}

func ensureUserToolchainSymlink(osProxy utils.OsProxy, p paths.Paths) error {
	if err := osProxy.MkdirAll(p.XcodeUserToolchainsDir(), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", p.XcodeUserToolchainsDir(), err)
	}

	symlink := p.DsymutilCasShimUserToolchainSymlink()
	target := p.DsymutilCasShimToolchainStagingDir()

	existing, err := os.Readlink(symlink)
	switch {
	case err == nil && existing == target:
		return nil
	case err == nil:
		// Points elsewhere; replace.
		if err := os.Remove(symlink); err != nil {
			return fmt.Errorf("remove stale symlink %s: %w", symlink, err)
		}
	case !os.IsNotExist(err):
		// Not a symlink but exists — remove and re-link.
		if _, statErr := os.Lstat(symlink); statErr == nil {
			if err := os.RemoveAll(symlink); err != nil {
				return fmt.Errorf("remove non-symlink %s: %w", symlink, err)
			}
		}
	}

	if err := os.Symlink(target, symlink); err != nil {
		return fmt.Errorf("symlink %s -> %s: %w", symlink, target, err)
	}

	return nil
}
