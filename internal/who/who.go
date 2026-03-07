package who

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	openv "github.com/organic-programming/grace-op/internal/env"
	"github.com/organic-programming/grace-op/internal/holons"
	sophiapb "github.com/organic-programming/sophia-who/gen/go/sophia_who/v1"
	"github.com/organic-programming/sophia-who/pkg/identity"
)

// List returns local and cached identities, preserving their origin labels.
func List(root string) (*sophiapb.ListIdentitiesResponse, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}

	var entries []*sophiapb.HolonEntry

	appendEntries := func(located []holons.LocalHolon) {
		for _, holon := range located {
			entries = append(entries, &sophiapb.HolonEntry{
				Identity:     toProto(holon.Identity),
				Origin:       holon.Origin,
				RelativePath: filepath.Clean(holon.RelativePath),
			})
		}
	}

	local, err := holons.DiscoverHolons(root)
	if err != nil {
		return nil, err
	}
	appendEntries(local)

	cached, err := holons.DiscoverCachedHolons()
	if err != nil {
		return nil, err
	}
	appendEntries(cached)

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].GetOrigin() == entries[j].GetOrigin() {
			if entries[i].GetRelativePath() == entries[j].GetRelativePath() {
				return entries[i].GetIdentity().GetUuid() < entries[j].GetIdentity().GetUuid()
			}
			return entries[i].GetRelativePath() < entries[j].GetRelativePath()
		}
		return entries[i].GetOrigin() < entries[j].GetOrigin()
	})

	return &sophiapb.ListIdentitiesResponse{Entries: entries}, nil
}

// Show resolves an identity by UUID or prefix, searching local first then cache.
func Show(target string) (*sophiapb.ShowIdentityResponse, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, fmt.Errorf("uuid is required")
	}

	local, err := holons.DiscoverHolons(openv.Root())
	if err != nil {
		return nil, err
	}
	cached, err := holons.DiscoverCachedHolons()
	if err != nil {
		return nil, err
	}

	matches := make([]holons.LocalHolon, 0)
	appendMatches := func(located []holons.LocalHolon) {
		for _, entry := range located {
			uuid := strings.TrimSpace(entry.Identity.UUID)
			if uuid == "" {
				continue
			}
			if uuid == target || strings.HasPrefix(uuid, target) {
				matches = append(matches, entry)
			}
		}
	}
	appendMatches(local)
	appendMatches(cached)
	if len(matches) == 0 {
		return nil, fmt.Errorf("holon not found: %s", target)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("uuid prefix %q is ambiguous", target)
	}

	path := matches[0].IdentityPath
	id, raw, err := identity.ReadHolonYAML(path)
	if err != nil {
		return nil, err
	}

	return &sophiapb.ShowIdentityResponse{
		Identity:   toProto(id),
		FilePath:   path,
		RawContent: string(raw),
	}, nil
}

// CreateFromJSON creates an identity from a non-interactive JSON payload.
func CreateFromJSON(raw string) (*sophiapb.CreateIdentityResponse, error) {
	req, err := parseCreateIdentityJSON(raw)
	if err != nil {
		return nil, err
	}
	return Create(req)
}

// CreateInteractive interactively scaffolds a new identity using stdin/stdout.
func CreateInteractive(in io.Reader, out io.Writer) (*sophiapb.CreateIdentityResponse, error) {
	scanner := bufio.NewScanner(in)
	id := identity.New()
	id.GeneratedBy = "op"

	fmt.Fprintln(out, "─── op new — New Holon Identity ───")
	fmt.Fprintf(out, "UUID: %s (generated)\n\n", id.UUID)

	req := &sophiapb.CreateIdentityRequest{}
	req.FamilyName = ask(scanner, out, "Family name (the function)")
	req.GivenName = ask(scanner, out, "Given name (the character)")
	req.Composer = ask(scanner, out, "Composer")
	req.Motto = ask(scanner, out, "Motto")

	fmt.Fprintln(out, "\nClade:")
	for i, clade := range identity.Clades {
		fmt.Fprintf(out, "  %d. %s\n", i+1, clade)
	}
	req.Clade = stringToClade(askChoice(scanner, out, "Choose clade", identity.Clades))

	fmt.Fprintln(out, "\nReproduction mode:")
	for i, reproduction := range identity.ReproductionModes {
		fmt.Fprintf(out, "  %d. %s\n", i+1, reproduction)
	}
	req.Reproduction = stringToReproduction(askChoice(scanner, out, "Choose reproduction mode", identity.ReproductionModes))

	req.Lang = askDefault(scanner, out, "Implementation language", "go")
	req.OutputDir = askDefault(scanner, out, "Output directory", filepath.Join("holons", slugFor(req.GivenName, req.FamilyName)))

	return Create(req)
}

// Create creates a new identity and writes holon.yaml.
func Create(req *sophiapb.CreateIdentityRequest) (*sophiapb.CreateIdentityResponse, error) {
	if err := validateCreateRequest(req); err != nil {
		return nil, err
	}

	id := identity.New()
	id.GeneratedBy = "op"
	id.GivenName = strings.TrimSpace(req.GetGivenName())
	id.FamilyName = strings.TrimSpace(req.GetFamilyName())
	id.Motto = strings.TrimSpace(req.GetMotto())
	id.Composer = strings.TrimSpace(req.GetComposer())
	id.Clade = cladeString(req.GetClade())
	id.Reproduction = reproductionString(req.GetReproduction())
	id.Lang = strings.TrimSpace(req.GetLang())
	if id.Lang == "" {
		id.Lang = "go"
	}
	if id.Reproduction == "" {
		id.Reproduction = "manual"
	}

	outputDir := strings.TrimSpace(req.GetOutputDir())
	if outputDir == "" {
		outputDir = filepath.Join("holons", slugFor(id.GivenName, id.FamilyName))
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create directory: %w", err)
	}

	outputPath := filepath.Join(outputDir, identity.ManifestFileName)
	if err := writeIdentityYAML(id, outputPath); err != nil {
		return nil, fmt.Errorf("write holon.yaml: %w", err)
	}

	return &sophiapb.CreateIdentityResponse{
		Identity: toProto(id),
		FilePath: outputPath,
	}, nil
}

func parseCreateIdentityJSON(raw string) (*sophiapb.CreateIdentityRequest, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("json payload is required")
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil, err
	}

	req := &sophiapb.CreateIdentityRequest{
		GivenName:    jsonString(payload, "given_name", "givenName"),
		FamilyName:   jsonString(payload, "family_name", "familyName"),
		Motto:        jsonString(payload, "motto"),
		Composer:     jsonString(payload, "composer"),
		Lang:         jsonString(payload, "lang"),
		OutputDir:    jsonString(payload, "output_dir", "outputDir"),
		Clade:        stringToClade(jsonString(payload, "clade")),
		Reproduction: stringToReproduction(jsonString(payload, "reproduction")),
	}
	return req, nil
}

func jsonString(payload map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		raw, ok := payload[key]
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err == nil {
			return value
		}
	}
	return ""
}

func ask(scanner *bufio.Scanner, out io.Writer, prompt string) string {
	for {
		fmt.Fprintf(out, "%s: ", prompt)
		scanner.Scan()
		answer := strings.TrimSpace(scanner.Text())
		if answer != "" {
			return answer
		}
		fmt.Fprintln(out, "  (required)")
	}
}

func askDefault(scanner *bufio.Scanner, out io.Writer, prompt, defaultVal string) string {
	if defaultVal != "" {
		fmt.Fprintf(out, "%s [%s]: ", prompt, defaultVal)
	} else {
		fmt.Fprintf(out, "%s: ", prompt)
	}
	scanner.Scan()
	answer := strings.TrimSpace(scanner.Text())
	if answer == "" {
		return defaultVal
	}
	return answer
}

func askChoice(scanner *bufio.Scanner, out io.Writer, prompt string, choices []string) string {
	for {
		answer := askDefault(scanner, out, prompt, "")
		for _, choice := range choices {
			if strings.EqualFold(answer, choice) {
				return choice
			}
		}
		for i, choice := range choices {
			if fmt.Sprintf("%d", i+1) == answer {
				return choice
			}
		}
		fmt.Fprintln(out, "  (choose a listed value or number)")
	}
}

func validateCreateRequest(req *sophiapb.CreateIdentityRequest) error {
	if req == nil {
		return fmt.Errorf("request is required")
	}
	if strings.TrimSpace(req.GetGivenName()) == "" {
		return fmt.Errorf("given_name is required")
	}
	if strings.TrimSpace(req.GetFamilyName()) == "" {
		return fmt.Errorf("family_name is required")
	}
	if strings.TrimSpace(req.GetMotto()) == "" {
		return fmt.Errorf("motto is required")
	}
	if strings.TrimSpace(req.GetComposer()) == "" {
		return fmt.Errorf("composer is required")
	}
	if cladeString(req.GetClade()) == "" {
		return fmt.Errorf("clade is required")
	}
	return nil
}

func slugFor(given, family string) string {
	slug := strings.ToLower(strings.TrimSpace(given + "-" + strings.TrimSuffix(family, "?")))
	slug = strings.ReplaceAll(slug, " ", "-")
	return strings.Trim(slug, "-")
}

func cladeString(value sophiapb.Clade) string {
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

func reproductionString(value sophiapb.ReproductionMode) string {
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

func writeIdentityYAML(id identity.Identity, outputPath string) error {
	if err := identity.WriteHolonYAML(id, outputPath); err != nil {
		return err
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "aliases:") {
			continue
		}
		filtered = append(filtered, line)
	}
	return os.WriteFile(outputPath, []byte(strings.Join(filtered, "\n")), 0o644)
}

func toProto(id identity.Identity) *sophiapb.HolonIdentity {
	return &sophiapb.HolonIdentity{
		Uuid:         id.UUID,
		GivenName:    id.GivenName,
		FamilyName:   id.FamilyName,
		Motto:        id.Motto,
		Composer:     id.Composer,
		Clade:        stringToClade(id.Clade),
		Status:       stringToStatus(id.Status),
		Born:         id.Born,
		Parents:      id.Parents,
		Reproduction: stringToReproduction(id.Reproduction),
		Aliases:      id.Aliases,
		GeneratedBy:  id.GeneratedBy,
		Lang:         id.Lang,
		ProtoStatus:  stringToStatus(id.ProtoStatus),
	}
}

func stringToClade(s string) sophiapb.Clade {
	switch strings.TrimSpace(s) {
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

func stringToStatus(s string) sophiapb.Status {
	switch strings.TrimSpace(s) {
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

func stringToReproduction(s string) sophiapb.ReproductionMode {
	switch strings.TrimSpace(s) {
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
