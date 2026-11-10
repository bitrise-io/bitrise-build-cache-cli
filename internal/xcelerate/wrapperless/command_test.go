//go:build unit

package wrapperless

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShortCommand(t *testing.T) {
	cases := []struct {
		name   string
		bucket Bucket
		want   string
	}{
		{
			name:   "build with scheme",
			bucket: Bucket{Action: PrimaryActionBuild, Scheme: "IceCubesApp"},
			want:   "build [IceCubesApp]",
		},
		{
			name:   "analyze with scheme and configuration",
			bucket: Bucket{Action: PrimaryActionAnalyze, Scheme: "IceCubesApp", Configuration: "Release"},
			want:   "analyze [IceCubesApp / Release]",
		},
		{
			name:   "archive with scheme and configuration",
			bucket: Bucket{Action: PrimaryActionArchive, Scheme: "Seek", Configuration: "Debug"},
			want:   "archive [Seek / Debug]",
		},
		{
			name:   "ide background reason bypasses scheme",
			bucket: Bucket{Reason: BucketReasonIDEBackground, Scheme: "Whatever"},
			want:   "ide background",
		},
		{
			name:   "cli implicit reason bypasses scheme",
			bucket: Bucket{Reason: BucketReasonCLIImplicit, Scheme: "Whatever"},
			want:   "cli run",
		},
		{
			name:   "no scheme falls back to bare verb",
			bucket: Bucket{Action: PrimaryActionBuild},
			want:   "build",
		},
		{
			name:   "no action defaults to build",
			bucket: Bucket{Scheme: "X"},
			want:   "build [X]",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, shortCommand(tc.bucket))
		})
	}
}

func TestFullCommand(t *testing.T) {
	cases := []struct {
		name   string
		bucket Bucket
		want   string
	}{
		{
			name:   "project + scheme + configuration",
			bucket: Bucket{Action: PrimaryActionAnalyze, Scheme: "IceCubesApp", Project: "IceCubesApp", Configuration: "Release"},
			want:   "xcodebuild analyze -project IceCubesApp -scheme IceCubesApp -configuration Release",
		},
		{
			name:   "workspace wins over project",
			bucket: Bucket{Action: PrimaryActionBuild, Scheme: "Seek", Workspace: "Seek", Project: "Seek"},
			want:   "xcodebuild build -workspace Seek -scheme Seek",
		},
		{
			name:   "scheme only, no container",
			bucket: Bucket{Action: PrimaryActionBuild, Scheme: "X"},
			want:   "xcodebuild build -scheme X",
		},
		{
			name:   "empty bucket falls back to shortCommand",
			bucket: Bucket{Action: PrimaryActionBuild},
			want:   "build",
		},
		{
			name:   "ide background reason bypasses argv",
			bucket: Bucket{Reason: BucketReasonIDEBackground, Scheme: "X", Project: "Y"},
			want:   "ide background",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, fullCommand(tc.bucket))
		})
	}
}

func TestFirstSubmatch_SignatureRegexes(t *testing.T) {
	sig := "Analyze project IceCubesApp with scheme IceCubesApp and Configuration Release"
	assert.Equal(t, "IceCubesApp", firstSubmatch(projectFromSignature, sig))
	assert.Equal(t, "Release", firstSubmatch(configurationFromSignature, sig))
	assert.Equal(t, "", firstSubmatch(workspaceFromSignature, sig))

	wsSig := "Build workspace Seek.xcworkspace with scheme Seek"
	assert.Equal(t, "Seek.xcworkspace", firstSubmatch(workspaceFromSignature, wsSig))
}
