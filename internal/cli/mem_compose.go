package cli

import (
	"context"
	"strings"
	"sync"

	sdkconnect "github.com/organic-programming/go-holons/pkg/connect"
	holonsgrpcclient "github.com/organic-programming/go-holons/pkg/grpcclient"
	"github.com/organic-programming/go-holons/pkg/transport"
	opv1 "github.com/organic-programming/grace-op/gen/go/op/v1"
	"github.com/organic-programming/grace-op/internal/server"

	"google.golang.org/grpc"
	grpcreflection "google.golang.org/grpc/reflection"
)

var (
	graceOPMemOnce     sync.Once
	graceOPMemListener *transport.MemListener
)

func init() {
	dialer := func(ctx context.Context) (*grpc.ClientConn, error) {
		return holonsgrpcclient.DialMem(ctx, ensureGraceOPMemListener())
	}

	sdkconnect.RegisterMemTarget("grace-op", dialer)
	sdkconnect.RegisterMemTarget("op", dialer)
}

func ensureGraceOPMemListener() *transport.MemListener {
	graceOPMemOnce.Do(func() {
		graceOPMemListener = transport.NewMemListener()

		grpcServer := grpc.NewServer()
		opv1.RegisterOPServiceServer(grpcServer, &server.Server{})
		grpcreflection.Register(grpcServer)

		go func() {
			_ = grpcServer.Serve(graceOPMemListener)
		}()
	})

	return graceOPMemListener
}

func hasMemComposer(holonName string) bool {
	switch strings.ToLower(strings.TrimSpace(holonName)) {
	case "grace-op", "op":
		return true
	default:
		return false
	}
}

func cmdGRPCMem(format Format, holonName string, args []string) int {
	return cmdGRPCConnected(format, "grpc+mem://"+holonName, holonName, args, sdkconnect.TransportMem)
}

func canonicalMethodName(method string) string {
	trimmed := strings.TrimSpace(method)
	if i := strings.LastIndex(trimmed, "/"); i >= 0 && i+1 < len(trimmed) {
		return trimmed[i+1:]
	}
	return trimmed
}
