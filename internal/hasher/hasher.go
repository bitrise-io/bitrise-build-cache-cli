package hasher

import (
	"crypto/sha256"
	"hash"

	remoteexecution "github.com/bitrise-io/bitrise-build-cache-cli/proto/build/bazel/remote/execution/v2"
	"github.com/zeebo/blake3"
)

func NewBlobHasher(hash remoteexecution.DigestFunction_Value) hash.Hash {
	//nolint:exhaustive
	switch hash {
	case remoteexecution.DigestFunction_SHA256:
		return sha256.New()
	case remoteexecution.DigestFunction_BLAKE3:
		return blake3.New()
	}

	return sha256.New()
}
