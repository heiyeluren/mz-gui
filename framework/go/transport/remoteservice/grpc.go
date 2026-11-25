package remoteservice

import (
	"context"
	"crypto/tls"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type GRPCOptions struct {
	Target             string
	Authority          string
	Timeout            time.Duration
	TLSConfig          *tls.Config
	AllowInsecureLocal bool
	DialOptions        []grpc.DialOption
}

func DialGRPC(ctx context.Context, options GRPCOptions) (*grpc.ClientConn, error) {
	if ctx == nil || strings.TrimSpace(options.Target) == "" || strings.ContainsAny(options.Target, "\r\n") {
		return nil, ErrInvalidRequest
	}
	callContext := ctx
	var cancel context.CancelFunc
	if options.Timeout > 0 {
		callContext, cancel = context.WithTimeout(ctx, options.Timeout)
		defer cancel()
	}
	dialOptions := make([]grpc.DialOption, 0, len(options.DialOptions)+3)
	if options.Authority != "" {
		if strings.ContainsAny(options.Authority, "\r\n") {
			return nil, ErrInvalidRequest
		}
		dialOptions = append(dialOptions, grpc.WithAuthority(options.Authority))
	}
	if options.TLSConfig != nil {
		dialOptions = append(dialOptions, grpc.WithTransportCredentials(credentials.NewTLS(options.TLSConfig)))
	} else {
		if !options.AllowInsecureLocal || !isLocalTarget(options.Target) {
			return nil, ErrPolicyDenied
		}
		dialOptions = append(dialOptions, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	dialOptions = append(dialOptions, options.DialOptions...)
	return grpc.DialContext(callContext, options.Target, dialOptions...)
}

func isLocalTarget(target string) bool {
	value := strings.TrimPrefix(target, "dns:///")
	value = strings.TrimPrefix(value, "passthrough:///")
	host, _, err := net.SplitHostPort(value)
	if err != nil {
		host = value
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || strings.HasPrefix(target, "unix:")
}
