package holons

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	openv "github.com/organic-programming/grace-op/internal/env"
	"github.com/organic-programming/sophia-who/pkg/identity"
)

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
	Origin       string
	Identity     identity.Identity
	IdentityPath string
	Manifest     *LoadedManifest
}

func KnownRoots() []string {
	return []string{openv.Root()}
}

func KnownRootLabels() []string {
	return []string{openv.Root()}
}

func DiscoverHolons(root string) ([]LocalHolon, error) {
	return discoverHolonsInRoot(root, "local", holonRelativePath)
}

func DiscoverLocalHolons() ([]LocalHolon, error) {
	return DiscoverHolons(openv.Root())
}

func DiscoverCachedHolons() ([]LocalHolon, error) {
	cacheDir := openv.CacheDir()
	info, err := os.Stat(cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}
	return discoverHolonsInRoot(cacheDir, "cached", cacheRelativePath)
}

func discoverHolonsInRoot(root, origin string, relPath func(string, string) string) ([]LocalHolon, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = openv.Root()
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(absRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}

	candidates := make(map[string]LocalHolon)
	orderedKeys := make([]string, 0)

	err = filepath.WalkDir(absRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}

		if d.IsDir() {
			if shouldSkipDiscoveryDir(absRoot, path, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != ManifestFileName {
			return nil
		}

		dir := filepath.Dir(path)
		absDir, err := filepath.Abs(dir)
		if err != nil {
			return nil
		}

		id, _, err := identity.ReadHolonYAML(path)
		if err != nil {
			return nil
		}

		manifest, err := LoadManifest(absDir)
		if err != nil {
			manifest = nil
		}

		entry := LocalHolon{
			Dir:          absDir,
			RelativePath: relPath(absRoot, absDir),
			Origin:       origin,
			Identity:     id,
			IdentityPath: path,
			Manifest:     manifest,
		}

		key := strings.TrimSpace(id.UUID)
		if key == "" {
			key = absDir
		}
		if existing, ok := candidates[key]; ok {
			if discoveryPathDepth(entry.RelativePath) < discoveryPathDepth(existing.RelativePath) {
				candidates[key] = entry
			}
			return nil
		}

		candidates[key] = entry
		orderedKeys = append(orderedKeys, key)
		return nil
	})
	if err != nil {
		return nil, err
	}

	entries := make([]LocalHolon, 0, len(candidates))
	for _, key := range orderedKeys {
		entry, ok := candidates[key]
		if ok {
			entries = append(entries, entry)
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

func shouldSkipDiscoveryDir(root, path, name string) bool {
	if path == root {
		return false
	}
	if name == ".git" || name == ".op" || name == "node_modules" || name == "vendor" || name == "build" {
		return true
	}
	return strings.HasPrefix(name, ".")
}

func discoveryPathDepth(rel string) int {
	trimmed := strings.Trim(strings.TrimSpace(filepath.ToSlash(rel)), "/")
	if trimmed == "" || trimmed == "." {
		return 0
	}
	return len(strings.Split(trimmed, "/"))
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

	if target, err := resolveTargetBySlug(trimmed); err == nil {
		return target, nil
	} else if !isTargetNotFound(err) {
		return nil, err
	}

	if target, err := resolveTargetByUUID(trimmed); err == nil {
		return target, nil
	} else if !isTargetNotFound(err) {
		return nil, err
	}

	return nil, fmt.Errorf("holon %q not found", trimmed)
}

func ResolveBinary(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", fmt.Errorf("holon %q not found", name)
	}

	if dir, ok, err := existingTargetDir(trimmed); err != nil {
		return "", err
	} else if ok {
		target, err := resolveDir(trimmed, dir)
		if err != nil {
			return "", err
		}
		if binaryPath := builtBinaryForTarget(target); binaryPath != "" {
			return binaryPath, nil
		}
		if systemPath := lookupBinaryOnSystem(binaryLookupNames(target, trimmed)...); systemPath != "" {
			return systemPath, nil
		}
		return "", fmt.Errorf("holon %q not found", name)
	}

	if target, err := resolveTargetBySlugFromOrigins(trimmed, true, false); err == nil {
		if binaryPath := builtBinaryForTarget(target); binaryPath != "" {
			return binaryPath, nil
		}
		if systemPath := lookupBinaryOnSystem(binaryLookupNames(target, trimmed)...); systemPath != "" {
			return systemPath, nil
		}
	} else if !isTargetNotFound(err) {
		return "", err
	}

	if systemPath := lookupBinaryOnSystem(trimmed); systemPath != "" {
		return systemPath, nil
	}

	if target, err := resolveTargetBySlugFromOrigins(trimmed, false, true); err == nil {
		if binaryPath := builtBinaryForTarget(target); binaryPath != "" {
			return binaryPath, nil
		}
		if systemPath := lookupBinaryOnSystem(binaryLookupNames(target, trimmed)...); systemPath != "" {
			return systemPath, nil
		}
	} else if !isTargetNotFound(err) {
		return "", err
	}

	if target, err := resolveTargetByUUID(trimmed); err == nil {
		if binaryPath := builtBinaryForTarget(target); binaryPath != "" {
			return binaryPath, nil
		}
		if systemPath := lookupBinaryOnSystem(binaryLookupNames(target, trimmed)...); systemPath != "" {
			return systemPath, nil
		}
	} else if !isTargetNotFound(err) {
		return "", err
	}

	return "", fmt.Errorf("holon %q not found", name)
}

func resolveTargetBySlug(ref string) (*Target, error) {
	return resolveTargetBySlugFromOrigins(ref, true, true)
}

func resolveTargetBySlugFromOrigins(ref string, includeLocal, includeCache bool) (*Target, error) {
	matches, err := collectSlugMatches(ref, includeLocal, includeCache)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("holon %q not found", ref)
	}
	if len(matches) > 1 {
		return nil, ambiguousHolonError(ref, matches)
	}
	return resolveDir(ref, matches[0].Dir)
}

func resolveTargetByUUID(ref string) (*Target, error) {
	matches, err := collectUUIDMatches(ref)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("holon %q not found", ref)
	}
	if len(matches) > 1 {
		return nil, ambiguousHolonError(ref, matches)
	}
	return resolveDir(ref, matches[0].Dir)
}

func collectSlugMatches(ref string, includeLocal, includeCache bool) ([]LocalHolon, error) {
	var combined []LocalHolon
	if includeLocal {
		local, err := DiscoverLocalHolons()
		if err != nil {
			return nil, err
		}
		combined = append(combined, filterHolonsBySlug(local, ref)...)
	}
	if includeCache {
		cached, err := DiscoverCachedHolons()
		if err != nil {
			return nil, err
		}
		combined = append(combined, filterHolonsBySlug(cached, ref)...)
	}
	return collapseMatchesByUUID(combined), nil
}

func collectUUIDMatches(ref string) ([]LocalHolon, error) {
	local, err := DiscoverLocalHolons()
	if err != nil {
		return nil, err
	}
	cached, err := DiscoverCachedHolons()
	if err != nil {
		return nil, err
	}
	combined := append(filterHolonsByUUID(local, ref), filterHolonsByUUID(cached, ref)...)
	return collapseMatchesByUUID(combined), nil
}

func filterHolonsBySlug(holons []LocalHolon, ref string) []LocalHolon {
	trimmed := strings.TrimSpace(ref)
	matches := make([]LocalHolon, 0)
	for _, holon := range holons {
		if filepath.Base(holon.Dir) == trimmed {
			matches = append(matches, holon)
		}
	}
	return matches
}

func filterHolonsByUUID(holons []LocalHolon, ref string) []LocalHolon {
	trimmed := strings.TrimSpace(ref)
	matches := make([]LocalHolon, 0)
	for _, holon := range holons {
		uuid := strings.TrimSpace(holon.Identity.UUID)
		if uuid == "" {
			continue
		}
		if uuid == trimmed || strings.HasPrefix(uuid, trimmed) {
			matches = append(matches, holon)
		}
	}
	return matches
}

func collapseMatchesByUUID(matches []LocalHolon) []LocalHolon {
	seen := make(map[string]struct{}, len(matches))
	collapsed := make([]LocalHolon, 0, len(matches))
	for _, match := range matches {
		key := strings.TrimSpace(match.Identity.UUID)
		if key == "" {
			key = match.Dir
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		collapsed = append(collapsed, match)
	}
	return collapsed
}

func ambiguousHolonError(ref string, matches []LocalHolon) error {
	var b strings.Builder
	fmt.Fprintf(&b, "ambiguous holon %q — found %d matches (different UUIDs):", ref, len(matches))
	for i, match := range matches {
		fmt.Fprintf(
			&b,
			"\n  %d. [%s]  %s  UUID %s",
			i+1,
			match.Origin,
			disambiguationPath(match),
			match.Identity.UUID,
		)
	}
	fmt.Fprintf(&b, "\nDisambiguate with a path or UUID:")
	for _, match := range matches {
		fmt.Fprintf(&b, "\n  op build %s", disambiguationPath(match))
		fmt.Fprintf(&b, "\n  op build %s", shortUUIDValue(match.Identity.UUID))
	}
	return errors.New(b.String())
}

func disambiguationPath(match LocalHolon) string {
	if match.Origin == "local" {
		rel := filepath.ToSlash(match.RelativePath)
		if rel == "" || rel == "." {
			return "./"
		}
		if strings.HasPrefix(rel, "./") {
			return rel
		}
		return "./" + rel
	}
	return filepath.ToSlash(match.RelativePath)
}

func shortUUIDValue(uuid string) string {
	if len(uuid) <= 8 {
		return uuid
	}
	return uuid[:8]
}

func builtBinaryForTarget(target *Target) string {
	if target == nil || target.Manifest == nil {
		return ""
	}
	binaryPath := target.Manifest.BinaryPath()
	if binaryPath == "" {
		return ""
	}
	if info, err := os.Stat(binaryPath); err == nil && !info.IsDir() {
		return binaryPath
	}
	return ""
}

func binaryLookupNames(target *Target, requested string) []string {
	names := []string{requested}
	if target != nil && target.Manifest != nil {
		names = append(names, target.Manifest.BinaryName())
	}
	if target != nil {
		names = append(names, filepath.Base(target.Dir))
	}
	return uniqueNonEmpty(names)
}

func lookupBinaryOnSystem(names ...string) string {
	for _, candidate := range uniqueNonEmpty(names) {
		installed := filepath.Join(openv.OPBIN(), candidate)
		if info, statErr := os.Stat(installed); statErr == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return installed
		}
		if path, lookErr := exec.LookPath(candidate); lookErr == nil {
			return path
		}
	}
	return ""
}

func isTargetNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "not found")
}

func DiscoverInPath() []string {
	names := []string{"op"}

	if holons, err := DiscoverLocalHolons(); err == nil {
		for _, holon := range holons {
			if holon.Manifest != nil && holon.Manifest.BinaryName() != "" {
				names = append(names, holon.Manifest.BinaryName())
				continue
			}
			names = append(names, filepath.Base(holon.Dir))
		}
	}
	if holons, err := DiscoverCachedHolons(); err == nil {
		for _, holon := range holons {
			if holon.Manifest != nil && holon.Manifest.BinaryName() != "" {
				names = append(names, holon.Manifest.BinaryName())
			}
		}
	}

	found := make([]string, 0, len(names))
	for _, name := range uniqueNonEmpty(names) {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if strings.HasPrefix(path, filepath.Clean(openv.OPBIN())+string(os.PathSeparator)) {
			continue
		}
		found = append(found, fmt.Sprintf("%s -> %s", name, path))
	}
	sort.Strings(found)
	return found
}

func DiscoverInOPBIN() []string {
	opbin := openv.OPBIN()
	entries, err := os.ReadDir(opbin)
	if err != nil {
		return nil
	}

	found := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.IsDir() {
			continue
		}
		if info.Mode()&0o111 == 0 {
			continue
		}
		path := filepath.Join(opbin, entry.Name())
		found = append(found, fmt.Sprintf("%s -> %s", entry.Name(), path))
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

func holonRelativePath(root, dir string) string {
	root = filepath.Clean(root)
	dir = filepath.Clean(dir)
	if rel, err := filepath.Rel(root, dir); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(dir)
}

func cacheRelativePath(root, dir string) string {
	root = filepath.Clean(root)
	dir = filepath.Clean(dir)
	if rel, err := filepath.Rel(root, dir); err == nil {
		if rel == "." {
			return "."
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return filepath.ToSlash(rel)
		}
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
	return openv.Root()
}

func hasKnownRoot(base string) bool {
	return filepath.Clean(base) == filepath.Clean(openv.Root())
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
