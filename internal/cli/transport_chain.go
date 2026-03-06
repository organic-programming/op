package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/organic-programming/sophia-who/pkg/identity"
)

// selectTransport determines the best transport for a target holon.
// Priority:
//  1. Already running (known endpoint) -> dial existing
//  2. Same language + loadable -> mem:// (lazy in-process)
//  3. Binary available locally -> stdio:// (ephemeral)
//  4. Network reachable -> tcp://
func selectTransport(holonName string) (scheme string, err error) {
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
