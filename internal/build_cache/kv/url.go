package kv

import (
	"fmt"
	"net/url"
)

func ParseUrlGRPC(s string) (string, bool, error) {
	parsed, err := url.ParseRequestURI(s)
	if err != nil {
		return "", false, fmt.Errorf("parse url: %w", err)
	}
	if parsed.Scheme != "grpc" && parsed.Scheme != "grpcs" {
		return "", false, fmt.Errorf("scheme must be grpc or grpcs")
	}
	if parsed.Port() == "" {
		return "", false, fmt.Errorf("must provide a port")
	}
	return parsed.Host, parsed.Scheme == "grpc", nil
}
