//go:build unit

package xcodeargs_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeargs"
)

func TestBuildOtherCFlagsValue(t *testing.T) {
	cases := []struct {
		name string
		in   xcodeargs.PrefixMapPaths
		want string
	}{
		{
			name: "empty struct → empty string",
			in:   xcodeargs.PrefixMapPaths{},
			want: "",
		},
		{
			name: "only Home set",
			in:   xcodeargs.PrefixMapPaths{Home: "/Users/x"},
			want: "-fdepscan-prefix-map=/Users/x=/^home",
		},
		{
			name: "only ProjectDir set",
			in:   xcodeargs.PrefixMapPaths{ProjectDir: "/work/app"},
			want: "-fdepscan-prefix-map=/work/app=/^src",
		},
		{
			name: "all four — narrowest-first order preserved",
			in: xcodeargs.PrefixMapPaths{
				Home:            "/Users/x",
				ProjectDir:      "/Users/x/dev/app",
				DerivedDataPath: "/Users/x/.bitrise/cache/xcode-dd/foo",
				ProjectTempDir:  "/Users/x/.bitrise/cache/xcode-ptd/foo",
			},
			want: "-fdepscan-prefix-map=/Users/x/.bitrise/cache/xcode-ptd/foo=/^obj " +
				"-fdepscan-prefix-map=/Users/x/.bitrise/cache/xcode-dd/foo=/^dd " +
				"-fdepscan-prefix-map=/Users/x/dev/app=/^src " +
				"-fdepscan-prefix-map=/Users/x=/^home",
		},
		{
			name: "subset — Home + ProjectDir only",
			in:   xcodeargs.PrefixMapPaths{Home: "/Users/x", ProjectDir: "/Users/x/dev/app"},
			want: "-fdepscan-prefix-map=/Users/x/dev/app=/^src " +
				"-fdepscan-prefix-map=/Users/x=/^home",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, xcodeargs.BuildOtherCFlagsValue(c.in))
		})
	}
}

func TestDefault_DerivedDataPath(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want string
	}{
		{"absent → empty", []string{"build"}, ""},
		{"space form", []string{"-derivedDataPath", "/tmp/dd"}, "/tmp/dd"},
		{"inline form", []string{"-derivedDataPath=/tmp/dd"}, "/tmp/dd"},
		{"last wins", []string{"-derivedDataPath", "/tmp/a", "-derivedDataPath=/tmp/b"}, "/tmp/b"},
		{
			"next-arg-is-flag → treated as missing value",
			[]string{"-derivedDataPath", "-scheme", "Foo"},
			"",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := xcodeargs.Default{OriginalArgs: c.argv}
			assert.Equal(t, c.want, d.DerivedDataPath())
		})
	}
}

func TestDefault_ProjectTempDir(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want string
	}{
		{"absent → empty", []string{"build"}, ""},
		{"build-setting form", []string{"PROJECT_TEMP_DIR=/tmp/ptd"}, "/tmp/ptd"},
		{
			"last wins",
			[]string{"PROJECT_TEMP_DIR=/tmp/a", "PROJECT_TEMP_DIR=/tmp/b"},
			"/tmp/b",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := xcodeargs.Default{OriginalArgs: c.argv}
			assert.Equal(t, c.want, d.ProjectTempDir())
		})
	}
}

func TestDefault_ProjectDir(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want string
	}{
		{"absent → empty", []string{"build"}, ""},
		{
			"-project flag → parent dir",
			[]string{"-project", "/work/app/App.xcodeproj"},
			"/work/app",
		},
		{
			"-workspace flag → parent dir",
			[]string{"-workspace", "/work/app/App.xcworkspace"},
			"/work/app",
		},
		{
			"-project takes precedence over -workspace when both present",
			[]string{
				"-project", "/work/proj/App.xcodeproj",
				"-workspace", "/work/wkspc/App.xcworkspace",
			},
			"/work/proj",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := xcodeargs.Default{OriginalArgs: c.argv}
			assert.Equal(t, c.want, d.ProjectDir())
		})
	}
}

func TestMergeOtherCFlagsValue(t *testing.T) {
	cases := []struct {
		name   string
		user   string
		suffix string
		want   string
	}{
		{"both empty → empty", "", "", ""},
		{
			"empty user + suffix → inherited prefix preserved",
			"",
			"-fdepscan-prefix-map=/a=/^x",
			"$(inherited) -fdepscan-prefix-map=/a=/^x",
		},
		{
			"user with $(inherited) collapses to single leading marker",
			"$(inherited) -Werror",
			"-fdepscan-prefix-map=/a=/^x",
			"$(inherited) -Werror -fdepscan-prefix-map=/a=/^x",
		},
		{
			"user without $(inherited) gets one prepended",
			"-Werror -O2",
			"-fdepscan-prefix-map=/a=/^x",
			"$(inherited) -Werror -O2 -fdepscan-prefix-map=/a=/^x",
		},
		{
			"user with duplicate $(inherited) markers collapsed",
			"$(inherited) $(inherited) -Werror",
			"",
			"$(inherited) -Werror",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, xcodeargs.MergeOtherCFlagsValue(c.user, c.suffix))
		})
	}
}

func TestDefault_UserOtherCFlags(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want string
	}{
		{"absent → empty", []string{"build"}, ""},
		{
			"single value",
			[]string{"OTHER_CFLAGS=$(inherited) -Werror"},
			"$(inherited) -Werror",
		},
		{
			"last wins",
			[]string{"OTHER_CFLAGS=-A", "OTHER_CFLAGS=-B"},
			"-B",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := xcodeargs.Default{OriginalArgs: c.argv}
			assert.Equal(t, c.want, d.UserOtherCFlags())
		})
	}
}

func TestDefault_ProjectDir_relativeWorkspaceResolvesToAbs(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	d := xcodeargs.Default{OriginalArgs: []string{"-workspace", "App.xcworkspace"}}

	// Bare filename → dir is "." → must resolve to the tempdir itself.
	assert.Equal(t, tmp, d.ProjectDir())
}

func TestDefault_ProjectDir_relativeProjectResolvesToAbs(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	d := xcodeargs.Default{OriginalArgs: []string{"-project", "sub/App.xcodeproj"}}

	assert.Equal(t, filepath.Join(tmp, "sub"), d.ProjectDir())
}

func TestDefault_ProjectDir_absoluteStaysAbsolute(t *testing.T) {
	// Regression: chdir to a scratch dir so a bug that re-resolved abs paths
	// against CWD would visibly move the answer.
	t.Chdir(t.TempDir())

	d := xcodeargs.Default{OriginalArgs: []string{"-workspace", "/work/app/App.xcworkspace"}}

	assert.Equal(t, "/work/app", d.ProjectDir())
}

func TestDefault_DerivedDataPath_relativeResolvesToAbs(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	d := xcodeargs.Default{OriginalArgs: []string{"-derivedDataPath", "dd"}}

	assert.Equal(t, filepath.Join(tmp, "dd"), d.DerivedDataPath())
}

func TestDefault_DerivedDataPath_absoluteStaysAbsolute(t *testing.T) {
	t.Chdir(t.TempDir())

	d := xcodeargs.Default{OriginalArgs: []string{"-derivedDataPath", "/tmp/dd"}}

	assert.Equal(t, "/tmp/dd", d.DerivedDataPath())
}

func TestDefault_ProjectTempDir_relativeResolvesToAbs(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	d := xcodeargs.Default{OriginalArgs: []string{"PROJECT_TEMP_DIR=ptd"}}

	assert.Equal(t, filepath.Join(tmp, "ptd"), d.ProjectTempDir())
}

func TestDefault_ProjectTempDir_absoluteStaysAbsolute(t *testing.T) {
	t.Chdir(t.TempDir())

	d := xcodeargs.Default{OriginalArgs: []string{"PROJECT_TEMP_DIR=/tmp/ptd"}}

	assert.Equal(t, "/tmp/ptd", d.ProjectTempDir())
}
