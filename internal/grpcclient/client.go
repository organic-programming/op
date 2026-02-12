// Package grpcclient connects to a holon's gRPC server and forwards calls.
// It uses gRPC reflection to discover available services and methods,
// making it work with ANY holon regardless of implementation language.
package grpcclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os/exec"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// CallResult holds the output of a gRPC call.
type CallResult struct {
	Service string `json:"service"`
	Method  string `json:"method"`
	Output  string `json:"output"`
}

// Dial connects to a gRPC server at the given address and calls a method.
// It uses server reflection to discover the service and method descriptors,
// so it works with any holon in any language.
func Dial(address, methodName string, inputJSON string) (*CallResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", address, err)
	}
	defer conn.Close()

	// Use reflection to discover services
	refClient := grpc_reflection_v1alpha.NewServerReflectionClient(conn)
	stream, err := refClient.ServerReflectionInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("reflection not available at %s: %w", address, err)
	}

	// List services
	if err := stream.Send(&grpc_reflection_v1alpha.ServerReflectionRequest{
		MessageRequest: &grpc_reflection_v1alpha.ServerReflectionRequest_ListServices{
			ListServices: "",
		},
	}); err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}

	listResp, err := stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("list services response: %w", err)
	}

	listResult := listResp.GetListServicesResponse()
	if listResult == nil {
		return nil, fmt.Errorf("no services found at %s", address)
	}

	// Find the matching method across all services
	for _, svc := range listResult.Service {
		// Skip reflection service itself
		if svc.Name == "grpc.reflection.v1alpha.ServerReflection" ||
			svc.Name == "grpc.reflection.v1.ServerReflection" {
			continue
		}

		desc, err := resolveService(stream, svc.Name)
		if err != nil {
			continue
		}

		methods := desc.Methods()
		for i := 0; i < methods.Len(); i++ {
			method := methods.Get(i)
			if string(method.Name()) == methodName {
				return callMethod(ctx, conn, desc, method, inputJSON)
			}
		}
	}

	// Method not found — list available methods for the error message
	var available []string
	for _, svc := range listResult.Service {
		if svc.Name == "grpc.reflection.v1alpha.ServerReflection" ||
			svc.Name == "grpc.reflection.v1.ServerReflection" {
			continue
		}
		desc, err := resolveService(stream, svc.Name)
		if err != nil {
			continue
		}
		methods := desc.Methods()
		for i := 0; i < methods.Len(); i++ {
			available = append(available, fmt.Sprintf("%s/%s", svc.Name, methods.Get(i).Name()))
		}
	}

	return nil, fmt.Errorf("method %q not found. Available: %v", methodName, available)
}

// ListMethods returns all available service methods at the given address.
func ListMethods(address string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", address, err)
	}
	defer conn.Close()

	refClient := grpc_reflection_v1alpha.NewServerReflectionClient(conn)
	stream, err := refClient.ServerReflectionInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("reflection not available: %w", err)
	}

	if err := stream.Send(&grpc_reflection_v1alpha.ServerReflectionRequest{
		MessageRequest: &grpc_reflection_v1alpha.ServerReflectionRequest_ListServices{
			ListServices: "",
		},
	}); err != nil {
		return nil, err
	}

	resp, err := stream.Recv()
	if err != nil {
		return nil, err
	}

	var methods []string
	for _, svc := range resp.GetListServicesResponse().Service {
		if svc.Name == "grpc.reflection.v1alpha.ServerReflection" ||
			svc.Name == "grpc.reflection.v1.ServerReflection" {
			continue
		}
		desc, err := resolveService(stream, svc.Name)
		if err != nil {
			continue
		}
		ms := desc.Methods()
		for i := 0; i < ms.Len(); i++ {
			methods = append(methods, fmt.Sprintf("%s/%s", svc.Name, ms.Get(i).Name()))
		}
	}
	return methods, nil
}

// --- Internal helpers ---

func resolveService(stream grpc_reflection_v1alpha.ServerReflection_ServerReflectionInfoClient, serviceName string) (protoreflect.ServiceDescriptor, error) {
	if err := stream.Send(&grpc_reflection_v1alpha.ServerReflectionRequest{
		MessageRequest: &grpc_reflection_v1alpha.ServerReflectionRequest_FileContainingSymbol{
			FileContainingSymbol: serviceName,
		},
	}); err != nil {
		return nil, err
	}

	resp, err := stream.Recv()
	if err != nil {
		return nil, err
	}

	fdResp := resp.GetFileDescriptorResponse()
	if fdResp == nil {
		return nil, fmt.Errorf("no file descriptor for %s", serviceName)
	}

	// Parse all file descriptors
	var files []*descriptorpb.FileDescriptorProto
	for _, b := range fdResp.FileDescriptorProto {
		fd := &descriptorpb.FileDescriptorProto{}
		if err := proto.Unmarshal(b, fd); err != nil {
			return nil, fmt.Errorf("unmarshal file descriptor: %w", err)
		}
		files = append(files, fd)
	}

	// Build a file descriptor set and resolve
	fds := &descriptorpb.FileDescriptorSet{File: files}
	fileDescs, err := protodesc.NewFiles(fds)
	if err != nil {
		return nil, fmt.Errorf("build file descriptors: %w", err)
	}

	svcDesc, err := fileDescs.FindDescriptorByName(protoreflect.FullName(serviceName))
	if err != nil {
		return nil, fmt.Errorf("find service %s: %w", serviceName, err)
	}

	sd, ok := svcDesc.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, fmt.Errorf("%s is not a service", serviceName)
	}

	return sd, nil
}

func callMethod(ctx context.Context, conn *grpc.ClientConn, svc protoreflect.ServiceDescriptor, method protoreflect.MethodDescriptor, inputJSON string) (*CallResult, error) {
	// Build the full method path: /package.ServiceName/MethodName
	fullMethod := fmt.Sprintf("/%s/%s", svc.FullName(), method.Name())

	// Create dynamic input message
	inputDesc := method.Input()
	inputMsg := dynamicpb.NewMessage(inputDesc)

	if inputJSON != "" && inputJSON != "{}" {
		if err := protojson.Unmarshal([]byte(inputJSON), inputMsg); err != nil {
			return nil, fmt.Errorf("parse input JSON: %w", err)
		}
	}

	// Create dynamic output message
	outputDesc := method.Output()
	outputMsg := dynamicpb.NewMessage(outputDesc)

	// Call the method
	if err := conn.Invoke(ctx, fullMethod, inputMsg, outputMsg); err != nil {
		return nil, fmt.Errorf("call %s: %w", fullMethod, err)
	}

	// Marshal output to JSON
	outputBytes, err := protojson.Marshal(outputMsg)
	if err != nil {
		return nil, fmt.Errorf("marshal output: %w", err)
	}

	// Pretty-print the JSON
	var pretty json.RawMessage
	if err := json.Unmarshal(outputBytes, &pretty); err != nil {
		return &CallResult{
			Service: string(svc.FullName()),
			Method:  string(method.Name()),
			Output:  string(outputBytes),
		}, nil
	}

	prettyBytes, _ := json.MarshalIndent(pretty, "", "  ")

	return &CallResult{
		Service: string(svc.FullName()),
		Method:  string(method.Name()),
		Output:  string(prettyBytes),
	}, nil
}

// DialStdio launches a holon binary with `serve --listen stdio://` and
// communicates over stdin/stdout pipes. This is the purest form of
// inter-holon gRPC — zero networking, zero port allocation.
func DialStdio(binaryPath, methodName, inputJSON string) (*CallResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "serve", "--listen", "stdio://")

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", binaryPath, err)
	}
	defer func() {
		cmd.Process.Kill() //nolint:errcheck
		cmd.Wait()         //nolint:errcheck
	}()

	// Wait for the server to write its HTTP/2 SETTINGS frame.
	// Reading the first byte proves the gRPC server is alive and
	// the pipe is functional. We prepend it back via MultiReader.
	firstByte := make([]byte, 1)
	readCh := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(stdoutPipe, firstByte)
		readCh <- err
	}()
	select {
	case err := <-readCh:
		if err != nil {
			return nil, fmt.Errorf("server did not start: %w", err)
		}
	case <-ctx.Done():
		return nil, fmt.Errorf("server startup timeout")
	}

	// Create a net.Conn backed by the process's stdin/stdout.
	// Prepend the first byte we already consumed.
	pConn := &pipeConn{
		reader: io.MultiReader(bytes.NewReader(firstByte), stdoutPipe),
		writer: stdinPipe,
	}

	// The pipe is a single connection — the dialer must return it exactly
	// once. Subsequent calls return an error (gRPC may try to reconnect).
	var dialOnce sync.Once
	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		var conn net.Conn
		dialOnce.Do(func() { conn = pConn })
		if conn == nil {
			return nil, fmt.Errorf("stdio pipe already consumed")
		}
		return conn, nil
	}

	// DialContext+WithBlock forces an immediate HTTP/2 handshake over
	// the pipe, which is required for single-connection transports.
	//nolint:staticcheck // DialContext is deprecated but needed for pipes.
	conn, err := grpc.DialContext(ctx,
		"passthrough:///stdio",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialer),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("create grpc client over stdio: %w", err)
	}
	defer conn.Close()

	// Use reflection to discover and call the method
	refClient := grpc_reflection_v1alpha.NewServerReflectionClient(conn)
	stream, err := refClient.ServerReflectionInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("reflection over stdio: %w", err)
	}

	if err := stream.Send(&grpc_reflection_v1alpha.ServerReflectionRequest{
		MessageRequest: &grpc_reflection_v1alpha.ServerReflectionRequest_ListServices{
			ListServices: "",
		},
	}); err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}

	listResp, err := stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("list services response: %w", err)
	}

	listResult := listResp.GetListServicesResponse()
	if listResult == nil {
		return nil, fmt.Errorf("no services found via stdio")
	}

	for _, svc := range listResult.Service {
		if svc.Name == "grpc.reflection.v1alpha.ServerReflection" ||
			svc.Name == "grpc.reflection.v1.ServerReflection" {
			continue
		}
		desc, err := resolveService(stream, svc.Name)
		if err != nil {
			continue
		}
		methods := desc.Methods()
		for i := 0; i < methods.Len(); i++ {
			method := methods.Get(i)
			if string(method.Name()) == methodName {
				return callMethod(ctx, conn, desc, method, inputJSON)
			}
		}
	}

	return nil, fmt.Errorf("method %q not found via stdio", methodName)
}

// pipeConn wraps an io.ReadCloser + io.WriteCloser as a net.Conn.
type pipeConn struct {
	reader interface{ Read([]byte) (int, error) }
	writer interface {
		Write([]byte) (int, error)
		Close() error
	}
}

func (c *pipeConn) Read(p []byte) (int, error)         { return c.reader.Read(p) }
func (c *pipeConn) Write(p []byte) (int, error)        { return c.writer.Write(p) }
func (c *pipeConn) Close() error                       { return c.writer.Close() }
func (c *pipeConn) LocalAddr() net.Addr                { return pipeAddr{} }
func (c *pipeConn) RemoteAddr() net.Addr               { return pipeAddr{} }
func (c *pipeConn) SetDeadline(_ time.Time) error      { return nil }
func (c *pipeConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *pipeConn) SetWriteDeadline(_ time.Time) error { return nil }

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "stdio://" }
