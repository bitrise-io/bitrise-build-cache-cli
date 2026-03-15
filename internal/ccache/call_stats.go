package ccache

import (
	"fmt"
	"time"
)

type callMethod string

const (
	CALL_METHOD_GET               callMethod = "Get"
	CALL_METHOD_PUT               callMethod = "Set"
	CALL_METHOD_REMOVE            callMethod = "Remove"
	CALL_METHOD_STOP              callMethod = "Stop"
	CALL_METHOD_SET_INVOCATION_ID callMethod = "SetInvocationID"
)

type callStats struct {
	start         time.Time
	method        callMethod
	key           string
	uploadBytes   int64
	downloadBytes int64
}

type statBuilder struct {
	stats callStats
}

func newStatBuilder(method callMethod) *statBuilder {
	return &statBuilder{
		stats: callStats{
			method: method,
			start:  time.Now(),
		},
	}
}

func (b *statBuilder) withKey(key string) {
	b.stats.key = key
}

func (b *statBuilder) withUploadBytes(bytes int64) *statBuilder {
	b.stats.uploadBytes = bytes

	return b
}

func (b *statBuilder) withDownloadBytes(bytes int64) *statBuilder {
	b.stats.downloadBytes = bytes

	return b
}

func (b *statBuilder) build() callStats {
	return b.stats
}

func (b *statBuilder) Prefix() string {
	if b.stats.key == "" {
		return fmt.Sprintf("[%s]", b.stats.method)
	}

	return fmt.Sprintf("[%s - %s]", b.stats.method, b.stats.key)
}
