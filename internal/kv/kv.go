package kv

import "strings"

const BlobKeyPrefix = "blob/"

func IsBlobKey(key string) bool {
	return strings.HasPrefix(key, BlobKeyPrefix)
}

func CreateBlobKey(key string) string {
	if IsBlobKey(key) {
		return key
	}

	return BlobKeyPrefix + key
}
