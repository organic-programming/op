package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	sdkconnect "github.com/organic-programming/go-holons/pkg/connect"
	internalgrpc "github.com/organic-programming/grace-op/internal/grpcclient"
)

const connectDispatchTimeout = 10 * time.Second

func oneShotConnectOptions(transport string) sdkconnect.ConnectOptions {
	return sdkconnect.ConnectOptions{
		Timeout:   connectDispatchTimeout,
		Transport: transport,
		Lifecycle: sdkconnect.LifecycleEphemeral,
		Start:     true,
	}
}

func runConnectedRPC(
	format Format,
	errPrefix string,
	holonName string,
	method string,
	inputJSON string,
	opts sdkconnect.ConnectOptions,
) int {
	conn, err := sdkconnect.ConnectWithOpts(holonName, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", errPrefix, err)
		return 1
	}
	defer func() { _ = sdkconnect.Disconnect(conn) }()

	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()

	result, err := internalgrpc.InvokeConn(ctx, conn, method, inputJSON)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", errPrefix, err)
		return 1
	}

	fmt.Println(formatRPCOutput(format, method, []byte(result.Output)))
	return 0
}

func cmdGRPCConnected(format Format, uri string, holonName string, args []string, transport string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "op grpc: method required")
		fmt.Fprintf(os.Stderr, "usage: op %s <method>\n", uri)
		return 1
	}

	method := args[0]
	inputJSON := "{}"
	if len(args) > 1 {
		inputJSON = args[1]
	}

	return runConnectedRPC(format, "op grpc", holonName, method, inputJSON, oneShotConnectOptions(transport))
}
