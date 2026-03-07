package holons

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/organic-programming/sophia-who/pkg/identity"
)

var searchRootCandidates = []string{
	"holons",
	"examples",
	"recipes",
	filepath.Join("organic-programming", "holons"),
	filepath.Join("organic-programming", "examples"),
	filepath.Join("organic-programming", "recipes"),
}

type Target struct {
	Ref          string
	Dir          string
	RelativePath string
	Identity     *identity.Identity
	IdentityPath string
	Manifest     *LoadedManifest
	ManifestErr  error
}

type LocalHolon struct {
	Dir          string
	RelativePath string
	Identity     identity.Identity
	IdentityPath string
	Manifest     *LoadedManifest
}

func KnownRoots() []string {
	base := workspaceRoot()
	seen := make(map[string]struct{}, len(searchRootCandidates))
	roots := make([]string, 0, len(searchRootCandidates))
	for _, candidate := range searchRootCandidates {
		cleaned := filepath.Clean(filepath.Join(base, candidate))
		if _, ok := seen[cleaned]; ok {
			continue
		}
		if info, err := os.Stat(cleaned); err == nil && info.IsDir() {
			roots = append(roots, cleaned)
			seen[cleaned] = struct{}{}
		}
	}
	return roots
}

func DiscoverLocalHolons() ([]LocalHolon, error) {
	var (
		entries []LocalHolon
		seen    = make(map[string]struct{})
	)

	for _, root := range KnownRoots() {
		located, err := identity.FindAllWithPaths(root)
		if err != nil {
			return nil, err
		}

		for _, holon := range located {
			dir := filepath.Dir(holon.Path)
			absDir, err := filepath.Abs(dir)
			if err != nil {
				return nil, err
			}
			if _, ok := seen[absDir]; ok {
				continue
			}
			seen[absDir] = struct{}{}

			manifest, err := LoadManifest(absDir)
			if err != nil {
				manifest = nil
			}

			entries = append(entries, LocalHolon{
				Dir:          absDir,
				RelativePath: holonRelativePath(root, dir),
				Identity:     holon.Identity,
				IdentityPath: holon.Path,
				Manifest:     manifest,
			})
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].RelativePath == entries[j].RelativePath {
			return entries[i].Identity.UUID < entries[j].Identity.UUID
		}
		return entries[i].RelativePath < entries[j].RelativePath
	})
	return entries, nil
}

func ResolveTarget(ref string) (*Target, error) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		trimmed = "."
	}

	if dir, ok, err := existingTargetDir(trimmed); err != nil {
		return nil, err
	} else if ok {
		return resolveDir(trimmed, dir)
	}

	for _, root := range KnownRoots() {
		candidate := filepath.Join(root, trimmed)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return resolveDir(trimmed, candidate)
		}
	}

	holons, err := DiscoverLocalHolons()
	if err != nil {
		return nil, err
	}

	for _, holon := range holons {
		if identityMatchesHolon(trimmed, holon.Identity, filepath.Base(holon.Dir)) {
			return resolveDir(trimmed, holon.Dir)
		}
	}

	return nil, fmt.Errorf("holon %q not found in %s", trimmed, strings.Join(KnownRoots(), ", "))
}

func ResolveBinary(name string) (string, error) {
	target, err := ResolveTarget(name)
	if err == nil {
		for _, candidate := range binaryCandidates(name, target) {
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}

	var names []string
	if target != nil {
		names = append(names, filepath.Base(target.Dir))
		if target.Manifest != nil {
			names = append(names, filepath.Base(target.Manifest.Manifest.Artifacts.Binary))
		}
	}
	if target != nil && target.Identity != nil {
		names = append(names, identitySlug(*target.Identity))
		names = append(names, strings.ToLower(strings.TrimSpace(target.Identity.GivenName)))
	}
	if target == nil || requestedMatchesCanonicalName(name, target) {
		names = append(names, name)
	}

	for _, candidate := range uniqueNonEmpty(names) {
		if path, lookErr := exec.LookPath(candidate); lookErr == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("holon %q not found", name)
}

func requestedMatchesCanonicalName(requested string, target *Target) bool {
	if target == nil {
		return true
	}
	trimmed := strings.TrimSpace(requested)
	if trimmed == "" {
		return false
	}
	for _, candidate := range uniqueNonEmpty([]string{
		filepath.Base(target.Dir),
		identitySlugValue(target),
		strings.ToLower(strings.TrimSpace(identityGivenName(target))),
	}) {
		if strings.EqualFold(trimmed, candidate) {
			return true
		}
	}
	if target.Manifest != nil && strings.EqualFold(trimmed, filepath.Base(target.Manifest.Manifest.Artifacts.Binary)) {
		return true
	}
	return false
}

func identitySlugValue(target *Target) string {
	if target == nil || target.Identity == nil {
		return ""
	}
	return identitySlug(*target.Identity)
}

func identityGivenName(target *Target) string {
	if target == nil || target.Identity == nil {
		return ""
	}
	return target.Identity.GivenName
}

func DiscoverInPath() []string {
	names := []string{"op"}

	if holons, err := DiscoverLocalHolons(); err == nil {
		for _, holon := range holons {
			names = append(names, holon.Identity.Aliases...)
			names = append(names, identitySlug(holon.Identity))
		}
	}

	found := make([]string, 0, len(names))
	for _, name := range uniqueNonEmpty(names) {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		found = append(found, fmt.Sprintf("%s -> %s", name, path))
	}
	sort.Strings(found)
	return found
}

func resolveDir(ref, dir string) (*Target, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	target := &Target{
		Ref:          ref,
		Dir:          absDir,
		RelativePath: workspaceRelativePath(absDir),
	}

	identityPath := filepath.Join(absDir, ManifestFileName)
	if id, _, err := identity.ReadHolonYAML(identityPath); err == nil {
		target.Identity = &id
		target.IdentityPath = identityPath
	}

	manifestPath := filepath.Join(absDir, ManifestFileName)
	if _, err := os.Stat(manifestPath); err == nil {
		manifest, loadErr := LoadManifest(absDir)
		if loadErr != nil {
			target.ManifestErr = loadErr
		} else {
			target.Manifest = manifest
		}
	}

	return target, nil
}

func existingTargetDir(ref string) (string, bool, error) {
	info, err := os.Stat(ref)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}

	if info.IsDir() {
		return ref, true, nil
	}

	switch filepath.Base(ref) {
	case ManifestFileName:
		return filepath.Dir(ref), true, nil
	default:
		return "", false, fmt.Errorf("%s is not a holon directory", ref)
	}
}

func binaryCandidates(requested string, target *Target) []string {
	var candidates []string
	if target == nil {
		return candidates
	}
	if target.Manifest != nil {
		candidates = append(candidates, target.Manifest.BinaryPath())
	}

	names := []string{requested, filepath.Base(target.Dir)}
	if target.Identity != nil {
		names = append(names, target.Identity.Aliases...)
		names = append(names, identitySlug(*target.Identity))
		names = append(names, strings.ToLower(strings.TrimSpace(target.Identity.GivenName)))
	}

	for _, name := range uniqueNonEmpty(names) {
		candidates = append(candidates, filepath.Join(target.Dir, name))
	}
	return uniqueNonEmpty(candidates)
}

func holonRelativePath(root, dir string) string {
	root = filepath.Clean(root)
	dir = filepath.Clean(dir)

	defaultHolonsRoot := filepath.Clean(filepath.Join(workspaceRoot(), "holons"))
	if root == defaultHolonsRoot {
		if rel, err := filepath.Rel(root, dir); err == nil {
			return filepath.ToSlash(rel)
		}
	}

	if rel, err := filepath.Rel(workspaceRoot(), dir); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(dir)
}

func workspaceRelativePath(path string) string {
	base := workspaceRoot()
	absPath, err := filepath.Abs(path)
	if err == nil {
		if rel, relErr := filepath.Rel(base, absPath); relErr == nil {
			return filepath.ToSlash(rel)
		}
	}
	if rel, err := filepath.Rel(base, path); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}

func workspaceRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}

	lastMatch := ""
	dir := cwd
	for {
		if hasKnownRoot(dir) {
			lastMatch = dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			if lastMatch != "" {
				return lastMatch
			}
			return cwd
		}
		dir = parent
	}
}

func hasKnownRoot(base string) bool {
	for _, candidate := range searchRootCandidates {
		info, err := os.Stat(filepath.Join(base, candidate))
		if err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func identityMatchesHolon(ref string, id identity.Identity, dirName string) bool {
	target := strings.ToLower(strings.TrimSpace(ref))
	if target == "" {
		return false
	}

	if strings.EqualFold(strings.TrimSpace(dirName), target) {
		return true
	}

	for _, alias := range id.Aliases {
		if strings.EqualFold(strings.TrimSpace(alias), target) {
			return true
		}
	}

	if strings.EqualFold(strings.TrimSpace(id.GivenName), target) {
		return true
	}

	return identitySlug(id) == target
}

func identitySlug(id identity.Identity) string {
	slug := strings.ToLower(strings.TrimSpace(id.GivenName + "-" + strings.TrimSuffix(id.FamilyName, "?")))
	slug = strings.ReplaceAll(slug, " ", "-")
	return strings.Trim(slug, "-")
}

func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}
