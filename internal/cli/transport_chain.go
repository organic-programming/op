package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/organic-programming/go-holons/pkg/transport"
	"github.com/organic-programming/sophia-who/pkg/identity"

	"gopkg.in/yaml.v3"
)

var supportedTransportSchemes = map[string]struct{}{
	"mem":   {},
	"stdio": {},
	"tcp":   {},
	"unix":  {},
	"ws":    {},
	"wss":   {},
}

// selectTransport determines the best transport for a target holon.
// Priority:
//  1. Already running (known endpoint) -> dial existing
//  2. Same language + loadable -> mem:// (lazy in-process)
//  3. Binary available locally -> stdio:// (ephemeral)
//  4. Network reachable -> tcp://
func selectTransport(holonName string) (scheme string, err error) {
	if override, found, err := lookupTransportOverride(holonName); err != nil {
		return "", err
	} else if found {
		return override, nil
	}

	binaryPath, err := resolveHolon(holonName)
	if err != nil {
		return "", fmt.Errorf("holon not reachable")
	}

	lang, err := readHolonLang(holonName, binaryPath)
	if err == nil && strings.EqualFold(lang, "go") {
		return "mem", nil
	}

	if binaryPath != "" {
		return "stdio", nil
	}

	return "", fmt.Errorf("holon not reachable")
}

func lookupTransportOverride(holonName string) (scheme string, found bool, err error) {
	configPath := ".holonconfig"
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read %s: %w", configPath, err)
	}

	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return "", false, fmt.Errorf("parse %s: %w", configPath, err)
	}

	if val, ok := lookupDotTransportKey(cfg, holonName); ok {
		scheme, err := normalizeTransportScheme(val)
		if err != nil {
			return "", false, err
		}
		return scheme, true, nil
	}

	if val, ok := lookupNestedTransportKey(cfg, holonName); ok {
		scheme, err := normalizeTransportScheme(val)
		if err != nil {
			return "", false, err
		}
		return scheme, true, nil
	}

	return "", false, nil
}

func lookupDotTransportKey(cfg map[string]any, holonName string) (string, bool) {
	for k, v := range cfg {
		if !strings.EqualFold(k, "transport."+holonName) {
			continue
		}
		s, ok := v.(string)
		return strings.TrimSpace(s), ok
	}
	return "", false
}

func lookupNestedTransportKey(cfg map[string]any, holonName string) (string, bool) {
	var raw any
	for k, v := range cfg {
		if strings.EqualFold(k, "transport") {
			raw = v
			break
		}
	}
	if raw == nil {
		return "", false
	}

	entries, ok := raw.(map[string]any)
	if !ok {
		return "", false
	}

	for k, v := range entries {
		if !strings.EqualFold(k, holonName) {
			continue
		}
		s, ok := v.(string)
		return strings.TrimSpace(s), ok
	}

	return "", false
}

func normalizeTransportScheme(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("invalid transport override %q", value)
	}

	scheme := strings.ToLower(strings.TrimSpace(transport.Scheme(trimmed)))
	if _, ok := supportedTransportSchemes[scheme]; !ok {
		return "", fmt.Errorf("invalid transport override %q", value)
	}

	return scheme, nil
}

func readHolonLang(holonName, binaryPath string) (string, error) {
	for _, holonPath := range holonMetadataCandidates(holonName, binaryPath) {
		data, err := os.ReadFile(holonPath)
		if err != nil {
			continue
		}
		id, _, err := identity.ParseFrontmatter(data)
		if err != nil {
			continue
		}
		if id.Lang != "" {
			return id.Lang, nil
		}
	}

	located, err := identity.FindAllWithPaths("holons")
	if err != nil {
		return "", err
	}
	for _, h := range located {
		if !identityMatchesHolon(holonName, h.Identity) {
			continue
		}
		if h.Identity.Lang != "" {
			return h.Identity.Lang, nil
		}
	}

	return "", fmt.Errorf("holon metadata not found")
}

func holonMetadataCandidates(holonName, binaryPath string) []string {
	candidates := []string{
		filepath.Join("holons", holonName, "HOLON.md"),
		filepath.Join("holons", "sophia-"+holonName, "HOLON.md"),
		filepath.Join("holons", "rhizome-"+holonName, "HOLON.md"),
		filepath.Join("holons", "abel-fishel-"+holonName, "HOLON.md"),
		filepath.Join("holons", "babel-fish-"+holonName, "HOLON.md"),
	}

	if binaryPath != "" {
		candidates = append(candidates,
			filepath.Join(filepath.Dir(binaryPath), "HOLON.md"),
			filepath.Join(filepath.Dir(filepath.Dir(binaryPath)), "HOLON.md"),
		)
	}

	seen := make(map[string]struct{}, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		c = filepath.Clean(c)
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}

func identityMatchesHolon(holonName string, id identity.Identity) bool {
	target := strings.ToLower(strings.TrimSpace(holonName))
	if target == "" {
		return false
	}

	for _, alias := range id.Aliases {
		if strings.EqualFold(strings.TrimSpace(alias), target) {
			return true
		}
	}

	given := strings.ToLower(strings.TrimSpace(id.GivenName))
	if given == target {
		return true
	}

	slug := strings.ToLower(strings.TrimSpace(id.GivenName + "-" + strings.TrimSuffix(id.FamilyName, "?")))
	slug = strings.ReplaceAll(slug, " ", "-")
	return slug == target
}
