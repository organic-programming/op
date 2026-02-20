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

	"github.com/organic-programming/go-holons/pkg/transport"
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
func (s *Server) CreateIdentity(ctx context.Context, req *sophiapb.CreateIdentityRequest) (*sophiapb.CreateIdentityResponse, error) {
	id := identity.New()

	if req.GivenName == "" || req.FamilyName == "" || req.Motto == "" || req.Composer == "" {
		return nil, fmt.Errorf("given_name, family_name, motto, and composer are required")
	}

	id.GivenName = req.GivenName
	id.FamilyName = req.FamilyName
	id.Motto = req.Motto
	id.Composer = req.Composer
	if clade := cladeFromProto(req.Clade); clade != "" {
		id.Clade = clade
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
	if reproduction := reproductionFromProto(req.Reproduction); reproduction != "" {
		id.Reproduction = reproduction
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

	return &sophiapb.CreateIdentityResponse{
		Identity: toProto(id),
		FilePath: outputPath,
	}, nil
}

// ListIdentities lists all known holon identities.
func (s *Server) ListIdentities(ctx context.Context, req *sophiapb.ListIdentitiesRequest) (*sophiapb.ListIdentitiesResponse, error) {
	root := req.RootDir
	if root == "" {
		root = "."
	}

	holons, err := identity.FindAll(root)
	if err != nil {
		return nil, err
	}

	entries := make([]*sophiapb.HolonEntry, 0, len(holons))
	for _, h := range holons {
		entries = append(entries, &sophiapb.HolonEntry{
			Identity: toProto(h),
			Origin:   "local",
		})
	}

	return &sophiapb.ListIdentitiesResponse{Entries: entries}, nil
}

// ShowIdentity retrieves a holon's identity by UUID.
func (s *Server) ShowIdentity(ctx context.Context, req *sophiapb.ShowIdentityRequest) (*sophiapb.ShowIdentityResponse, error) {
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

	return &sophiapb.ShowIdentityResponse{
		Identity:   toProto(id),
		FilePath:   path,
		RawContent: string(data),
	}, nil
}

// PinVersion pins version metadata to a holon.
func (s *Server) PinVersion(ctx context.Context, req *sophiapb.PinVersionRequest) (*sophiapb.PinVersionResponse, error) {
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

	return &sophiapb.PinVersionResponse{Identity: toProto(id)}, nil
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
		Uuid:           id.UUID,
		GivenName:      id.GivenName,
		FamilyName:     id.FamilyName,
		Motto:          id.Motto,
		Composer:       id.Composer,
		Clade:          cladeToProto(id.Clade),
		Status:         statusToProto(id.Status),
		Born:           id.Born,
		Parents:        id.Parents,
		Reproduction:   reproductionToProto(id.Reproduction),
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
		ProtoStatus:    statusToProto(id.ProtoStatus),
	}
}

func cladeToProto(value string) sophiapb.Clade {
	switch strings.ToLower(value) {
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

func cladeFromProto(value sophiapb.Clade) string {
	switch value {
	case sophiapb.Clade_DETERMINISTIC_PURE:
		return "deterministic/pure"
	case sophiapb.Clade_DETERMINISTIC_STATEFUL:
		return "deterministic/stateful"
	case sophiapb.Clade_DETERMINISTIC_IO_BOUND:
		return "deterministic/io_bound"
	case sophiapb.Clade_PROBABILISTIC_GENERATIVE:
		return "probabilistic/generative"
	case sophiapb.Clade_PROBABILISTIC_PERCEPTUAL:
		return "probabilistic/perceptual"
	case sophiapb.Clade_PROBABILISTIC_ADAPTIVE:
		return "probabilistic/adaptive"
	default:
		return ""
	}
}

func statusToProto(value string) sophiapb.Status {
	switch strings.ToLower(value) {
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
	switch strings.ToLower(value) {
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

func reproductionFromProto(value sophiapb.ReproductionMode) string {
	switch value {
	case sophiapb.ReproductionMode_MANUAL:
		return "manual"
	case sophiapb.ReproductionMode_ASSISTED:
		return "assisted"
	case sophiapb.ReproductionMode_AUTOMATIC:
		return "automatic"
	case sophiapb.ReproductionMode_AUTOPOIETIC:
		return "autopoietic"
	case sophiapb.ReproductionMode_BRED:
		return "bred"
	default:
		return ""
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
