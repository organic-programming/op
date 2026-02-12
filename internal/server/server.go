// Package server implements OP's gRPC service — the network facet.
package server

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Organic-Programming/op/internal/transport"
	"github.com/Organic-Programming/sophia-who/pkg/identity"

	pb "github.com/Organic-Programming/op/proto"

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

	holons, err := identity.FindAll(root)
	if err != nil {
		return nil, err
	}

	entries := make([]*pb.HolonEntry, 0, len(holons))
	for _, h := range holons {
		entries = append(entries, &pb.HolonEntry{
			Identity: toProto(h),
			Origin:   "local",
		})
	}

	pathBinaries := discoverInPath()

	return &pb.DiscoverResponse{
		Entries:      entries,
		PathBinaries: pathBinaries,
	}, nil
}

// Invoke dispatches a command to a holon by name.
func (s *Server) Invoke(ctx context.Context, req *pb.InvokeRequest) (*pb.InvokeResponse, error) {
	binary, err := resolveHolon(req.Holon)
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
func (s *Server) CreateIdentity(ctx context.Context, req *pb.CreateIdentityRequest) (*pb.CreateIdentityResponse, error) {
	id := identity.New()

	if req.GivenName == "" || req.FamilyName == "" || req.Motto == "" || req.Composer == "" {
		return nil, fmt.Errorf("given_name, family_name, motto, and composer are required")
	}

	id.GivenName = req.GivenName
	id.FamilyName = req.FamilyName
	id.Motto = req.Motto
	id.Composer = req.Composer
	if req.Clade != "" {
		id.Clade = req.Clade
	}
	if req.Lang != "" {
		id.Lang = req.Lang
	}
	if len(req.Aliases) > 0 {
		id.Aliases = req.Aliases
	}
	if req.WrappedLicense != "" {
		id.WrappedLicense = req.WrappedLicense
	}
	if req.Reproduction != "" {
		id.Reproduction = req.Reproduction
	}

	outputDir := req.OutputDir
	if outputDir == "" {
		dirName := strings.ToLower(id.GivenName + "-" + strings.TrimSuffix(id.FamilyName, "?"))
		dirName = strings.ReplaceAll(dirName, " ", "-")
		outputDir = filepath.Join(".holon", dirName)
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("cannot create directory: %w", err)
	}

	outputPath := filepath.Join(outputDir, "HOLON.md")
	if err := identity.WriteHolonMD(id, outputPath); err != nil {
		return nil, err
	}

	return &pb.CreateIdentityResponse{
		Identity: toProto(id),
		FilePath: outputPath,
	}, nil
}

// ListIdentities lists all known holon identities.
func (s *Server) ListIdentities(ctx context.Context, req *pb.ListIdentitiesRequest) (*pb.ListIdentitiesResponse, error) {
	root := req.RootDir
	if root == "" {
		root = "."
	}

	holons, err := identity.FindAll(root)
	if err != nil {
		return nil, err
	}

	entries := make([]*pb.HolonEntry, 0, len(holons))
	for _, h := range holons {
		entries = append(entries, &pb.HolonEntry{
			Identity: toProto(h),
			Origin:   "local",
		})
	}

	return &pb.ListIdentitiesResponse{Entries: entries}, nil
}

// ShowIdentity retrieves a holon's identity by UUID.
func (s *Server) ShowIdentity(ctx context.Context, req *pb.ShowIdentityRequest) (*pb.ShowIdentityResponse, error) {
	path, err := identity.FindByUUID(".", req.Uuid)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}

	id, _, err := identity.ParseFrontmatter(data)
	if err != nil {
		return nil, err
	}

	return &pb.ShowIdentityResponse{
		Identity:   toProto(id),
		FilePath:   path,
		RawContent: string(data),
	}, nil
}

// PinVersion pins version metadata to a holon.
func (s *Server) PinVersion(ctx context.Context, req *pb.PinVersionRequest) (*pb.PinVersionResponse, error) {
	path, err := identity.FindByUUID(".", req.Uuid)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}

	id, _, err := identity.ParseFrontmatter(data)
	if err != nil {
		return nil, err
	}

	if req.BinaryPath != "" {
		id.BinaryPath = req.BinaryPath
	}
	if req.BinaryVersion != "" {
		id.BinaryVersion = req.BinaryVersion
	}
	if req.GitTag != "" {
		id.GitTag = req.GitTag
	}
	if req.GitCommit != "" {
		id.GitCommit = req.GitCommit
	}
	if req.Os != "" {
		id.OS = req.Os
	}
	if req.Arch != "" {
		id.Arch = req.Arch
	}

	if err := identity.WriteHolonMD(id, path); err != nil {
		return nil, err
	}

	return &pb.PinVersionResponse{Identity: toProto(id)}, nil
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

func toProto(id identity.Identity) *pb.HolonIdentity {
	return &pb.HolonIdentity{
		Uuid:           id.UUID,
		GivenName:      id.GivenName,
		FamilyName:     id.FamilyName,
		Motto:          id.Motto,
		Composer:       id.Composer,
		Clade:          id.Clade,
		Status:         id.Status,
		Born:           id.Born,
		Parents:        id.Parents,
		Reproduction:   id.Reproduction,
		BinaryPath:     id.BinaryPath,
		BinaryVersion:  id.BinaryVersion,
		GitTag:         id.GitTag,
		GitCommit:      id.GitCommit,
		Os:             id.OS,
		Arch:           id.Arch,
		Dependencies:   id.Dependencies,
		Aliases:        id.Aliases,
		WrappedLicense: id.WrappedLicense,
		GeneratedBy:    id.GeneratedBy,
		Lang:           id.Lang,
		ProtoStatus:    id.ProtoStatus,
	}
}

// discoverInPath looks for known holon binaries in $PATH.
func discoverInPath() []string {
	known := []string{"who", "atlas", "translate", "op"}
	var found []string
	for _, name := range known {
		if p, err := exec.LookPath(name); err == nil {
			found = append(found, fmt.Sprintf("%s → %s", name, p))
		}
	}
	return found
}

// resolveHolon finds a holon binary by name.
func resolveHolon(name string) (string, error) {
	aliases := map[string]string{
		"who":       "who",
		"atlas":     "atlas",
		"translate": "translate",
	}

	binName := name
	if mapped, ok := aliases[name]; ok {
		binName = mapped
	}

	candidates := []string{
		filepath.Join("holons", name, binName),
		filepath.Join("holons", "sophia-"+name, binName),
		filepath.Join("holons", "rhizome-"+name, binName),
		filepath.Join("holons", "babel-fish-"+name, binName),
	}

	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}

	if p, err := exec.LookPath(binName); err == nil {
		return p, nil
	}

	return "", fmt.Errorf("holon %q not found", name)
}
