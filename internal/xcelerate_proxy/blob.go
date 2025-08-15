package xcelerate_proxy

import (
	"encoding/hex"

	remoteexecution "github.com/bitrise-io/bitrise-build-cache-cli/proto/build/bazel/remote/execution/v2"
	llvmcas "github.com/bitrise-io/bitrise-build-cache-cli/proto/llvm/cas"
)

const digestFunction = remoteexecution.DigestFunction_BLAKE3

func createLLVMCasKey(id *llvmcas.CASDataID) string {
	return "xcelerate-cas-" + hex.EncodeToString(id.GetId())
}

func createLLVMKVKey(key []byte) string {
	return "xcelerate-kv-" + hex.EncodeToString(key)
}

type blob struct {
	Data       []byte
	References [][]byte
}
