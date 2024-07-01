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

	isSecure := parsed.Scheme == "grpcs"

	if parsed.Scheme != "grpc" && parsed.Scheme != "grpcs" {
		return "", false, fmt.Errorf("scheme must be grpc or grpcs")
	}

	host := parsed.Host
	if parsed.Port() == "" {
		if isSecure {
			host += ":80"
		} else {
			host += ":443"
		}
	}

	return host, isSecure, nil
}
