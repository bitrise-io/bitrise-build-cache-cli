package grpcutil

import (
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// parseGRPCAddress parses a gRPC address and returns the address without a protocol prefix
// and a boolean indicating if TLS is enabled (true for grpcs://, false for grpc:// or no prefix)
// For grpcs addresses without a port, :443 is added as the default port.
func parseGRPCAddress(addr string) (string, bool) {
	addr = strings.TrimSpace(addr)

	switch {
	case strings.HasPrefix(addr, "grpcs://"):
		addr = strings.TrimPrefix(addr, "grpcs://")
		if !strings.Contains(addr, ":") {
			addr += ":443"
		}

		return addr, true
	case strings.HasPrefix(addr, "grpc://"):
		return strings.TrimPrefix(addr, "grpc://"), false
	default:
		return addr, false
	}
}

func NewGRPCClient(addr string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	address, isTLS := parseGRPCAddress(addr)
	var creds credentials.TransportCredentials
	if isTLS {
		creds = credentials.NewClientTLSFromCert(nil, "")
	} else {
		creds = insecure.NewCredentials()
	}

	return grpc.NewClient(address, append([]grpc.DialOption{grpc.WithTransportCredentials(creds)}, opts...)...) //nolint:wrapcheck
}
