package kv

import (
	"fmt"
	"net/url"
)

func ParseURLGRPC(s string) (string, bool, error) {
	parsed, err := url.ParseRequestURI(s)
	if err != nil {
		return "", false, fmt.Errorf("parse url: %w", err)
	}
	if parsed.Scheme != "grpc" && parsed.Scheme != "grpcs" {
		return "", false, fmt.Errorf("scheme must be grpc or grpcs")
	}

	host := parsed.Host
	if parsed.Port() == "" {
		if parsed.Scheme == "grpc" {
			host += ":80"
		} else {
			host += ":443"
		}
	}

	return host, parsed.Scheme == "grpc", nil
}
