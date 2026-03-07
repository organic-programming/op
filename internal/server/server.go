// Package server implements OP's gRPC service — the network facet.
package server

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/organic-programming/go-holons/pkg/transport"
	"github.com/organic-programming/grace-op/internal/holons"
	"github.com/organic-programming/grace-op/internal/who"
	sophiapb "github.com/organic-programming/sophia-who/gen/go/sophia_who/v1"
	"github.com/organic-programming/sophia-who/pkg/identity"

	pb "github.com/organic-programming/grace-op/gen/go/op/v1"

	"google.golang.org/grpc"
	grpcReflection "google.golang.org/grpc/reflection"
)

// Server implements the OPService gRPC interface.
type Server struct {
	pb.UnimplementedOPServiceServer
}

// --- OP-native RPCs ---

// Discover scans for all known holons.
func (s *Server) Discover(ctx context.Context, req *pb.DiscoverRequest) (*pb.DiscoverResponse, error) {
	root := req.RootDir
	if root == "" {
		root = "."
	}

	localHolons, err := identity.FindAllWithPaths(root)
	if err != nil {
		return nil, err
	}

	entries := make([]*pb.HolonEntry, 0, len(localHolons))
	for _, h := range localHolons {
		entries = append(entries, &pb.HolonEntry{
			Identity: toProto(h.Identity),
			Origin:   "local",
		})
	}

	pathBinaries := holons.DiscoverInPath()

	return &pb.DiscoverResponse{
		Entries:      entries,
		PathBinaries: pathBinaries,
	}, nil
}

// Invoke dispatches a command to a holon by name.
func (s *Server) Invoke(ctx context.Context, req *pb.InvokeRequest) (*pb.InvokeResponse, error) {
	binary, err := holons.ResolveBinary(req.Holon)
	if err != nil {
		return &pb.InvokeResponse{
			ExitCode: 1,
			Stderr:   fmt.Sprintf("holon %q not found", req.Holon),
		}, nil
	}

	cmd := exec.CommandContext(ctx, binary, req.Args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	exitCode := int32(0)
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = int32(exitErr.ExitCode())
		} else {
			return nil, fmt.Errorf("failed to run %s: %w", req.Holon, err)
		}
	}

	return &pb.InvokeResponse{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}, nil
}

// --- Promoted identity RPCs (delegate to Sophia's API) ---

// CreateIdentity creates a new holon identity.
func (s *Server) CreateIdentity(ctx context.Context, req *sophiapb.CreateIdentityRequest) (*sophiapb.CreateIdentityResponse, error) {
	return who.Create(req)
}

// ListIdentities lists all known holon identities.
func (s *Server) ListIdentities(ctx context.Context, req *sophiapb.ListIdentitiesRequest) (*sophiapb.ListIdentitiesResponse, error) {
	root := "."
	if req != nil && req.GetRootDir() != "" {
		root = req.GetRootDir()
	}
	return who.List(root)
}

// ShowIdentity retrieves a holon's identity by UUID.
func (s *Server) ShowIdentity(ctx context.Context, req *sophiapb.ShowIdentityRequest) (*sophiapb.ShowIdentityResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("uuid is required")
	}
	return who.Show(req.GetUuid())
}

// ListenAndServe starts the gRPC server on the given transport URI.
// Supported URIs: tcp://<host>:<port>, unix://<path>, stdio://
func ListenAndServe(listenURI string, reflect bool) error {
	lis, err := transport.Listen(listenURI)
	if err != nil {
		return fmt.Errorf("listen %s: %w", listenURI, err)
	}

	s := grpc.NewServer()
	pb.RegisterOPServiceServer(s, &Server{})
	if reflect {
		grpcReflection.Register(s)
	}

	mode := "reflection ON"
	if !reflect {
		mode = "reflection OFF"
	}
	log.Printf("OP gRPC server listening on %s (%s)", listenURI, mode)
	return s.Serve(lis)
}

// --- Helpers ---

func toProto(id identity.Identity) *sophiapb.HolonIdentity {
	return &sophiapb.HolonIdentity{
		Uuid:         id.UUID,
		GivenName:    id.GivenName,
		FamilyName:   id.FamilyName,
		Motto:        id.Motto,
		Composer:     id.Composer,
		Clade:        cladeToProto(id.Clade),
		Status:       statusToProto(id.Status),
		Born:         id.Born,
		Parents:      id.Parents,
		Reproduction: reproductionToProto(id.Reproduction),
		Aliases:      id.Aliases,
		GeneratedBy:  id.GeneratedBy,
		Lang:         id.Lang,
		ProtoStatus:  statusToProto(id.ProtoStatus),
	}
}

func cladeToProto(value string) sophiapb.Clade {
	switch lowerTrim(value) {
	case "deterministic/pure":
		return sophiapb.Clade_DETERMINISTIC_PURE
	case "deterministic/stateful":
		return sophiapb.Clade_DETERMINISTIC_STATEFUL
	case "deterministic/io_bound":
		return sophiapb.Clade_DETERMINISTIC_IO_BOUND
	case "probabilistic/generative":
		return sophiapb.Clade_PROBABILISTIC_GENERATIVE
	case "probabilistic/perceptual":
		return sophiapb.Clade_PROBABILISTIC_PERCEPTUAL
	case "probabilistic/adaptive":
		return sophiapb.Clade_PROBABILISTIC_ADAPTIVE
	default:
		return sophiapb.Clade_CLADE_UNSPECIFIED
	}
}

func statusToProto(value string) sophiapb.Status {
	switch lowerTrim(value) {
	case "draft":
		return sophiapb.Status_DRAFT
	case "stable":
		return sophiapb.Status_STABLE
	case "deprecated":
		return sophiapb.Status_DEPRECATED
	case "dead":
		return sophiapb.Status_DEAD
	default:
		return sophiapb.Status_STATUS_UNSPECIFIED
	}
}

func reproductionToProto(value string) sophiapb.ReproductionMode {
	switch lowerTrim(value) {
	case "manual":
		return sophiapb.ReproductionMode_MANUAL
	case "assisted":
		return sophiapb.ReproductionMode_ASSISTED
	case "automatic":
		return sophiapb.ReproductionMode_AUTOMATIC
	case "autopoietic":
		return sophiapb.ReproductionMode_AUTOPOIETIC
	case "bred":
		return sophiapb.ReproductionMode_BRED
	default:
		return sophiapb.ReproductionMode_REPRODUCTION_UNSPECIFIED
	}
}

func lowerTrim(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
