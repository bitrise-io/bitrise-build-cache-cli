package jobsummary_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/jobsummary"
)

func bytesOf(v int64) *int64 { return &v }

func invocation(url string, opts ...func(*jobsummary.Invocation)) jobsummary.Invocation {
	i := jobsummary.Invocation{
		Success:         true,
		Command:         "build",
		DownloadedBytes: bytesOf(0),
		UploadedBytes:   bytesOf(0),
		InvocationURL:   url,
	}
	for _, opt := range opts {
		opt(&i)
	}

	return i
}

func summaryFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "summary.md")
	t.Setenv("GITHUB_STEP_SUMMARY", path)

	return path
}

func TestJobRunningSeveralToolsGetsABlockEach(t *testing.T) {
	path := summaryFile(t)

	rows := []jobsummary.Invocation{
		invocation("https://app.bitrise.io/build-cache/invocations/xcode/a", func(i *jobsummary.Invocation) {
			i.Command = "build -scheme WordPress"
			i.DownloadedBytes = bytesOf(3_438_200_000)
			i.Duration = 151300 * time.Millisecond
		}),
		invocation("https://app.bitrise.io/build-cache/invocations/ccache/b", func(i *jobsummary.Invocation) {
			i.Command = "ccache"
			i.DownloadedBytes = bytesOf(8_600_000)
			i.UploadedBytes = bytesOf(118_000_000)
		}),
	}
	for i, r := range rows {
		written, err := jobsummary.Write(jobsummary.Block(r), fmt.Sprintf("inv-%d", i))
		require.NoError(t, err)
		assert.True(t, written)
	}

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	// One heading, then a block per invocation: its command, then its own table.
	assert.Equal(t, 1, strings.Count(string(content), "### ⚡️ Bitrise Build Cache"))
	assert.Contains(t, string(content), "**build -scheme WordPress**")
	assert.Contains(t, string(content), "| ✅ | Healthy | 3,438.2 MB | 0 MB | 2m 31.3s |")
	// No duration for ccache: the session spans the build, not one command.
	assert.Contains(t, string(content), "**ccache**")
	assert.Contains(t, string(content), "| ✅ | Low reuse | 8.6 MB | 118 MB |  |")
}

func TestAnInvocationReportingTwiceReplacesItsBlock(t *testing.T) {
	path := summaryFile(t)
	url := "https://app.bitrise.io/build-cache/invocations/xcode/a"

	for _, mb := range []int64{1_000_000, 2_000_000} {
		i := invocation(url, func(i *jobsummary.Invocation) { i.DownloadedBytes = bytesOf(mb) })
		_, err := jobsummary.Write(jobsummary.Block(i), "xcode-a")
		require.NoError(t, err)
	}

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	assert.Equal(t, 1, strings.Count(string(content), "| ✅"))
	assert.Contains(t, string(content), "2 MB")
	assert.NotContains(t, string(content), "1 MB")
}

func TestAnotherStepsContentIsLeftAlone(t *testing.T) {
	path := summaryFile(t)
	require.NoError(t, os.WriteFile(path, []byte("## Test results\n\nAll green.\n"), 0o600))

	i := invocation("https://app.bitrise.io/build-cache/invocations/xcode/a")
	_, err := jobsummary.Write(jobsummary.Block(i), "xcode-a")
	require.NoError(t, err)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(content), "All green.")
}

func TestNothingIsWrittenOffGitHubActions(t *testing.T) {
	t.Setenv("GITHUB_STEP_SUMMARY", "")

	written, err := jobsummary.Write(jobsummary.Block(invocation("url")), "xcode-a")
	require.NoError(t, err)
	assert.False(t, written)
}

// Same ladder as invocationCacheStatus.ts, so a row here reads like a row there.
func TestCacheStatusFollowsTheWebUIsOrder(t *testing.T) {
	tests := map[string]struct {
		invocation jobsummary.Invocation
		want       string
	}{
		"baseline wins over any transfer": {
			invocation: jobsummary.Invocation{BenchmarkPhase: "baseline", DownloadedBytes: bytesOf(5), UploadedBytes: bytesOf(1)},
			want:       "Baseline",
		},
		"nothing moved":                {invocation: jobsummary.Invocation{DownloadedBytes: bytesOf(0), UploadedBytes: bytesOf(0)}, want: "No activity"},
		"never reported":               {invocation: jobsummary.Invocation{}, want: "No activity"},
		"warmup":                       {invocation: jobsummary.Invocation{BenchmarkPhase: "warmup", DownloadedBytes: bytesOf(1), UploadedBytes: bytesOf(5)}, want: "Warming up"},
		"served what it was asked for": {invocation: jobsummary.Invocation{DownloadedBytes: bytesOf(5), UploadedBytes: bytesOf(5)}, want: "Healthy"},
		"stored more than it served":   {invocation: jobsummary.Invocation{DownloadedBytes: bytesOf(1), UploadedBytes: bytesOf(5)}, want: "Low reuse"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, jobsummary.CacheStatus(tt.invocation))
		})
	}
}

func TestAFailedBuildStillReportsWhatTheCacheDid(t *testing.T) {
	block := jobsummary.Block(invocation("url", func(i *jobsummary.Invocation) {
		i.Success = false
		i.DownloadedBytes = bytesOf(10_000_000)
	}))

	assert.Contains(t, block, "| ❌ |")
	assert.Contains(t, block, "Healthy")
}

func TestAnnotationLevelFollowsTheStatus(t *testing.T) {
	healthy := jobsummary.Annotation(jobsummary.Invocation{
		Command:         "compileDebugKotlin",
		DownloadedBytes: bytesOf(3_438_200_000),
		UploadedBytes:   bytesOf(0),
		InvocationURL:   "https://app.bitrise.io/build-cache/invocations/gradle/a",
	})
	assert.Equal(t,
		"::notice title=Bitrise Build Cache::Healthy — compileDebugKotlin: 3,438.2 MB downloaded, 0 MB uploaded."+
			"%0Ahttps://app.bitrise.io/build-cache/invocations/gradle/a",
		healthy)

	// Low reuse is the one worth acting on, so it is the one that warns.
	lowReuse := jobsummary.Annotation(jobsummary.Invocation{
		Command:         "compileDebugKotlin",
		DownloadedBytes: bytesOf(8_600_000),
		UploadedBytes:   bytesOf(118_000_000),
	})
	assert.Contains(t, lowReuse, "::warning title=Bitrise Build Cache::Low reuse — ")
	assert.NotContains(t, lowReuse, "%0A")
}
