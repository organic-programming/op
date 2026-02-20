package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	holonsgrpcclient "github.com/organic-programming/go-holons/pkg/grpcclient"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// callViaStdio launches a holon binary with `serve --listen stdio://`,
// establishes a gRPC connection over the pipe, calls the specified RPC,
// and sends SIGTERM after receiving the response.
func callViaStdio(binaryPath string, method string, input []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, cmd, err := holonsgrpcclient.DialStdio(ctx, binaryPath)
	if err != nil {
		return nil, fmt.Errorf("dial stdio: %w", err)
	}
	defer conn.Close()

	output, callErr := invokeViaReflection(ctx, conn, method, input)
	cleanupErr := terminateStdioProcess(cmd)

	if callErr != nil {
		return nil, callErr
	}
	if cleanupErr != nil {
		return nil, cleanupErr
	}
	return output, nil
}

func terminateStdioProcess(cmd *exec.Cmd) error {
	if cmd == nil {
		return nil
	}
	if cmd.Process != nil {
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("send SIGTERM: %w", err)
		}
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	select {
	case err := <-waitCh:
		if err == nil || errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return fmt.Errorf("wait process exit: %w", err)
	case <-time.After(3 * time.Second):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-waitCh
		return fmt.Errorf("process did not exit after SIGTERM")
	}
}

func invokeViaReflection(ctx context.Context, conn *grpc.ClientConn, method string, input []byte) ([]byte, error) {
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

	targetMethod := canonicalMethodName(method)
	var available []string
	for _, svc := range listResult.Service {
		if svc.Name == "grpc.reflection.v1alpha.ServerReflection" ||
			svc.Name == "grpc.reflection.v1.ServerReflection" {
			continue
		}

		desc, err := resolveReflectedService(stream, svc.Name)
		if err != nil {
			continue
		}

		methods := desc.Methods()
		for i := 0; i < methods.Len(); i++ {
			m := methods.Get(i)
			available = append(available, fmt.Sprintf("%s/%s", svc.Name, m.Name()))
			if string(m.Name()) == targetMethod {
				return invokeReflectedMethod(ctx, conn, desc, m, input)
			}
		}
	}

	return nil, fmt.Errorf("method %q not found via stdio. available: %v", method, available)
}

func invokeReflectedMethod(
	ctx context.Context,
	conn *grpc.ClientConn,
	svc protoreflect.ServiceDescriptor,
	method protoreflect.MethodDescriptor,
	input []byte,
) ([]byte, error) {
	inputDesc := method.Input()
	inputMsg := dynamicpb.NewMessage(inputDesc)
	trimmed := strings.TrimSpace(string(input))
	if trimmed == "" {
		trimmed = "{}"
	}
	if err := protojson.Unmarshal([]byte(trimmed), inputMsg); err != nil {
		return nil, fmt.Errorf("parse input JSON: %w", err)
	}

	outputDesc := method.Output()
	outputMsg := dynamicpb.NewMessage(outputDesc)
	fullMethod := fmt.Sprintf("/%s/%s", svc.FullName(), method.Name())
	if err := conn.Invoke(ctx, fullMethod, inputMsg, outputMsg); err != nil {
		return nil, fmt.Errorf("call %s: %w", fullMethod, err)
	}

	out, err := protojson.Marshal(outputMsg)
	if err != nil {
		return nil, fmt.Errorf("marshal output: %w", err)
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, out, "", "  "); err != nil {
		return out, nil
	}
	return pretty.Bytes(), nil
}

func resolveReflectedService(
	stream grpc_reflection_v1alpha.ServerReflection_ServerReflectionInfoClient,
	serviceName string,
) (protoreflect.ServiceDescriptor, error) {
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

	filesByName := make(map[string]*descriptorpb.FileDescriptorProto)
	var queue []string
	for _, b := range fdResp.FileDescriptorProto {
		fd := &descriptorpb.FileDescriptorProto{}
		if err := proto.Unmarshal(b, fd); err != nil {
			return nil, fmt.Errorf("unmarshal file descriptor: %w", err)
		}
		name := fd.GetName()
		if name == "" {
			continue
		}
		if _, exists := filesByName[name]; exists {
			continue
		}
		filesByName[name] = fd
		queue = append(queue, name)
	}

	for i := 0; i < len(queue); i++ {
		fd := filesByName[queue[i]]
		for _, dep := range fd.GetDependency() {
			if _, exists := filesByName[dep]; exists {
				continue
			}

			depFiles, err := resolveReflectedFileByName(stream, dep)
			if err != nil && !strings.HasPrefix(dep, "protos/") {
				depFiles, err = resolveReflectedFileByName(stream, "protos/"+dep)
			}
			if err != nil {
				return nil, err
			}

			aliasSourceName := ""
			for _, depFD := range depFiles {
				name := depFD.GetName()
				if name == "" || name == dep {
					continue
				}
				if strings.HasSuffix(name, dep) {
					aliasSourceName = name
					break
				}
			}

			resolvedDepName := false
			for _, depFD := range depFiles {
				name := depFD.GetName()
				if name == "" {
					continue
				}
				if name == dep {
					resolvedDepName = true
				}
				if aliasSourceName != "" && name == aliasSourceName {
					continue
				}
				if _, exists := filesByName[name]; exists {
					continue
				}
				filesByName[name] = depFD
				queue = append(queue, name)
			}

			if !resolvedDepName && aliasSourceName != "" {
				for _, depFD := range depFiles {
					name := depFD.GetName()
					if name != aliasSourceName {
						continue
					}
					aliased := proto.Clone(depFD).(*descriptorpb.FileDescriptorProto)
					aliased.Name = proto.String(dep)
					filesByName[dep] = aliased
					queue = append(queue, dep)
					break
				}
			}
		}
	}

	fds := &descriptorpb.FileDescriptorSet{
		File: make([]*descriptorpb.FileDescriptorProto, 0, len(queue)),
	}
	for _, name := range queue {
		fds.File = append(fds.File, filesByName[name])
	}

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

func resolveReflectedFileByName(
	stream grpc_reflection_v1alpha.ServerReflection_ServerReflectionInfoClient,
	filename string,
) ([]*descriptorpb.FileDescriptorProto, error) {
	if err := stream.Send(&grpc_reflection_v1alpha.ServerReflectionRequest{
		MessageRequest: &grpc_reflection_v1alpha.ServerReflectionRequest_FileByFilename{
			FileByFilename: filename,
		},
	}); err != nil {
		return nil, fmt.Errorf("request descriptor %s: %w", filename, err)
	}

	resp, err := stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("read descriptor %s: %w", filename, err)
	}
	fdResp := resp.GetFileDescriptorResponse()
	if fdResp == nil {
		return nil, fmt.Errorf("no file descriptor response for %s", filename)
	}

	files := make([]*descriptorpb.FileDescriptorProto, 0, len(fdResp.FileDescriptorProto))
	for _, b := range fdResp.FileDescriptorProto {
		fd := &descriptorpb.FileDescriptorProto{}
		if err := proto.Unmarshal(b, fd); err != nil {
			return nil, fmt.Errorf("unmarshal descriptor %s: %w", filename, err)
		}
		files = append(files, fd)
	}
	return files, nil
}
