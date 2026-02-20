package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	holonsgrpcclient "github.com/organic-programming/go-holons/pkg/grpcclient"
	"github.com/organic-programming/go-holons/pkg/transport"
	sophiapb "github.com/organic-programming/sophia-who/gen/go/sophia_who/v1"

	opserver "github.com/organic-programming/grace-op/internal/server"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type memHolonComposer struct {
	register func(*grpc.Server)
	callRPC  func(context.Context, *grpc.ClientConn, string, string) (string, error)

	once     sync.Once
	listener *transport.MemListener
}

var sophiaMemComposer = &memHolonComposer{
	register: registerSophiaWhoService,
	callRPC:  callSophiaWhoRPC,
}

// memComposeRegistry maps supported holon names to their in-process composer.
var memComposeRegistry = map[string]*memHolonComposer{
	"who":        sophiaMemComposer,
	"sophia":     sophiaMemComposer,
	"sophia-who": sophiaMemComposer,
}

type sophiaWhoAdapter struct {
	sophiapb.UnimplementedSophiaWhoServiceServer
	delegate *opserver.Server
}

func (a *sophiaWhoAdapter) CreateIdentity(ctx context.Context, req *sophiapb.CreateIdentityRequest) (*sophiapb.CreateIdentityResponse, error) {
	return a.delegate.CreateIdentity(ctx, req)
}

func (a *sophiaWhoAdapter) ShowIdentity(ctx context.Context, req *sophiapb.ShowIdentityRequest) (*sophiapb.ShowIdentityResponse, error) {
	return a.delegate.ShowIdentity(ctx, req)
}

func (a *sophiaWhoAdapter) ListIdentities(ctx context.Context, req *sophiapb.ListIdentitiesRequest) (*sophiapb.ListIdentitiesResponse, error) {
	return a.delegate.ListIdentities(ctx, req)
}

func (a *sophiaWhoAdapter) PinVersion(ctx context.Context, req *sophiapb.PinVersionRequest) (*sophiapb.PinVersionResponse, error) {
	return a.delegate.PinVersion(ctx, req)
}

func registerSophiaWhoService(s *grpc.Server) {
	sophiapb.RegisterSophiaWhoServiceServer(s, &sophiaWhoAdapter{
		delegate: &opserver.Server{},
	})
}

func dialMemHolon(ctx context.Context, holonName string) (*grpc.ClientConn, error) {
	composer, err := resolveMemComposer(holonName)
	if err != nil {
		return nil, err
	}

	composer.once.Do(func() {
		composer.listener = transport.NewMemListener()
		s := grpc.NewServer()
		composer.register(s)
		go func() {
			_ = s.Serve(composer.listener)
		}()
	})

	conn, err := holonsgrpcclient.DialMem(ctx, composer.listener)
	if err != nil {
		return nil, fmt.Errorf("dial mem composition for %q: %w", holonName, err)
	}
	return conn, nil
}

func resolveMemComposer(holonName string) (*memHolonComposer, error) {
	key := strings.ToLower(strings.TrimSpace(holonName))
	composer, ok := memComposeRegistry[key]
	if !ok {
		return nil, fmt.Errorf("mem composition not available for holon %q", holonName)
	}
	return composer, nil
}

func cmdGRPCMem(format Format, holonName string, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "op grpc: method required")
		fmt.Fprintf(os.Stderr, "usage: op grpc://%s <method>\n", holonName)
		return 1
	}

	method := args[0]
	inputJSON := "{}"
	if len(args) > 1 {
		inputJSON = args[1]
	}

	output, err := callViaMem(holonName, method, inputJSON)
	if err != nil {
		fmt.Fprintf(os.Stderr, "op grpc: %v\n", err)
		return 1
	}

	fmt.Println(formatRPCOutput(format, method, []byte(output)))
	return 0
}

func callViaMem(holonName, methodName, inputJSON string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := dialMemHolon(ctx, holonName)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	composer, err := resolveMemComposer(holonName)
	if err != nil {
		return "", err
	}
	return composer.callRPC(ctx, conn, methodName, inputJSON)
}

func callSophiaWhoRPC(ctx context.Context, conn *grpc.ClientConn, methodName, inputJSON string) (string, error) {
	method := canonicalMethodName(methodName)
	client := sophiapb.NewSophiaWhoServiceClient(conn)

	switch method {
	case "CreateIdentity":
		req := &sophiapb.CreateIdentityRequest{}
		if err := unmarshalProtoJSON(inputJSON, req); err != nil {
			return "", fmt.Errorf("parse input JSON: %w", err)
		}
		resp, err := client.CreateIdentity(ctx, req)
		if err != nil {
			return "", err
		}
		return marshalProtoJSON(resp)
	case "ShowIdentity":
		req := &sophiapb.ShowIdentityRequest{}
		if err := unmarshalProtoJSON(inputJSON, req); err != nil {
			return "", fmt.Errorf("parse input JSON: %w", err)
		}
		resp, err := client.ShowIdentity(ctx, req)
		if err != nil {
			return "", err
		}
		return marshalProtoJSON(resp)
	case "ListIdentities":
		req := &sophiapb.ListIdentitiesRequest{}
		if err := unmarshalProtoJSON(inputJSON, req); err != nil {
			return "", fmt.Errorf("parse input JSON: %w", err)
		}
		resp, err := client.ListIdentities(ctx, req)
		if err != nil {
			return "", err
		}
		return marshalProtoJSON(resp)
	case "PinVersion":
		req := &sophiapb.PinVersionRequest{}
		if err := unmarshalProtoJSON(inputJSON, req); err != nil {
			return "", fmt.Errorf("parse input JSON: %w", err)
		}
		resp, err := client.PinVersion(ctx, req)
		if err != nil {
			return "", err
		}
		return marshalProtoJSON(resp)
	default:
		return "", fmt.Errorf("method %q not found via mem", methodName)
	}
}

func canonicalMethodName(method string) string {
	trimmed := strings.TrimSpace(method)
	if i := strings.LastIndex(trimmed, "/"); i >= 0 && i+1 < len(trimmed) {
		return trimmed[i+1:]
	}
	return trimmed
}

func unmarshalProtoJSON(input string, msg proto.Message) error {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		trimmed = "{}"
	}
	return protojson.Unmarshal([]byte(trimmed), msg)
}

func marshalProtoJSON(msg proto.Message) (string, error) {
	out, err := protojson.Marshal(msg)
	if err != nil {
		return "", err
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, out, "", "  "); err != nil {
		return string(out), nil
	}
	return pretty.String(), nil
}
