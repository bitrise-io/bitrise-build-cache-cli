package kv

import (
	"fmt"
	"net/url"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
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
			host += ":443"
		} else {
			host += ":80"
		}
	}

	return host, !isSecure, nil
}

func NewGRPCClient(addr string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	host, insecureGRPC, err := ParseURLGRPC(addr)
	if err != nil {
		return nil, fmt.Errorf("the url grpc[s]://host:port format, %q is invalid: %w", addr, err)
	}

	var creds credentials.TransportCredentials
	if insecureGRPC {
		creds = insecure.NewCredentials()
	} else {
		creds = credentials.NewClientTLSFromCert(nil, "")
	}

	return grpc.NewClient(host, append([]grpc.DialOption{grpc.WithTransportCredentials(creds)}, opts...)...) //nolint:wrapcheck
}
