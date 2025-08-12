package llvm

import (
	"encoding/hex"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/kv"
	remoteexecution "github.com/bitrise-io/bitrise-build-cache-cli/proto/build/bazel/remote/execution/v2"
	llvmcas "github.com/bitrise-io/bitrise-build-cache-cli/proto/llvm/cas"
)

const LLVMHash = remoteexecution.DigestFunction_BLAKE3

func CreateLLVMCasKey(id *llvmcas.CASDataID) string {
	return kv.CreateBlobKey("llvm-cas-" + hex.EncodeToString(id.GetId()))
}

func CreateLLVMKVKey(key []byte) string {
	return kv.CreateBlobKey("llvm-kv-" + hex.EncodeToString(key))
}

type LLVMBlob struct {
	Data       []byte
	References [][]byte
}
