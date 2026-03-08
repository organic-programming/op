package holons

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	SchemaV0         = "holon/v0"
	KindNative       = "native"
	KindWrapper      = "wrapper"
	KindComposite    = "composite"
	RunnerGoModule   = "go-module"
	RunnerCMake      = "cmake"
	RunnerRecipe     = "recipe"
	ManifestFileName = "holon.yaml"
)

type Manifest struct {
	// Identity fields — present in holon.yaml but not used by lifecycle.
	Schema      string   `yaml:"schema"`
	UUID        string   `yaml:"uuid,omitempty"`
	GivenName   string   `yaml:"given_name,omitempty"`
	FamilyName  string   `yaml:"family_name,omitempty"`
	Motto       string   `yaml:"motto,omitempty"`
	Composer    string   `yaml:"composer,omitempty"`
	Clade       string   `yaml:"clade,omitempty"`
	Status      string   `yaml:"status,omitempty"`
	Born        string   `yaml:"born,omitempty"`
	Lang        string   `yaml:"lang,omitempty"`
	Aliases     []string `yaml:"aliases,omitempty"`
	ProtoStatus string   `yaml:"proto_status,omitempty"`

	// Lineage fields.
	Parents      []string `yaml:"parents,omitempty"`
	Reproduction string   `yaml:"reproduction,omitempty"`
	GeneratedBy  string   `yaml:"generated_by,omitempty"`

	// Description.
	Description string  `yaml:"description,omitempty"`
	Skills      []Skill `yaml:"skills,omitempty"`

	// Operational fields — used by lifecycle.
	Kind      string        `yaml:"kind"`
	Platforms []string      `yaml:"platforms,omitempty"`
	Build     BuildConfig   `yaml:"build"`
	Requires  Requires      `yaml:"requires,omitempty"`
	Delegates Delegates     `yaml:"delegates,omitempty"`
	Artifacts ArtifactPaths `yaml:"artifacts"`

	// Contract fields — not used by lifecycle.
	Contract interface{} `yaml:"contract,omitempty"`
}

type Skill struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	When        string   `yaml:"when,omitempty"`
	Steps       []string `yaml:"steps"`
}

type BuildConfig struct {
	Runner   string                  `yaml:"runner"`
	Main     string                  `yaml:"main,omitempty"`
	Defaults *RecipeDefaults         `yaml:"defaults,omitempty"`
	Members  []RecipeMember          `yaml:"members,omitempty"`
	Targets  map[string]RecipeTarget `yaml:"targets,omitempty"`
}

// RecipeDefaults provides default target and mode for recipe builds.
type RecipeDefaults struct {
	Target string `yaml:"target,omitempty"`
	Mode   string `yaml:"mode,omitempty"`
}

// RecipeMember is a named build participant in a composite holon.
type RecipeMember struct {
	ID   string `yaml:"id"`
	Path string `yaml:"path"`
	Type string `yaml:"type"` // "holon" or "component"
}

// RecipeTarget defines the build steps for a specific platform.
type RecipeTarget struct {
	Steps []RecipeStep `yaml:"steps"`
}

// RecipeStep is one step in a recipe build plan.
// Exactly one field should be set.
type RecipeStep struct {
	BuildMember string          `yaml:"build_member,omitempty"`
	Exec        *RecipeStepExec `yaml:"exec,omitempty"`
	Copy        *RecipeStepCopy `yaml:"copy,omitempty"`
	AssertFile  *RecipeStepFile `yaml:"assert_file,omitempty"`
}

// RecipeStepExec runs a command with an explicit argv and working directory.
type RecipeStepExec struct {
	Cwd  string   `yaml:"cwd"`
	Argv []string `yaml:"argv"`
}

// RecipeStepCopy copies a file from one manifest-relative path to another.
type RecipeStepCopy struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// RecipeStepFile verifies a manifest-relative file exists.
type RecipeStepFile struct {
	Path string `yaml:"path"`
}

type Requires struct {
	Commands []string `yaml:"commands,omitempty"`
	Files    []string `yaml:"files,omitempty"`
}

type Delegates struct {
	Commands []string `yaml:"commands,omitempty"`
}

type ArtifactPaths struct {
	Binary          string                       `yaml:"binary"`
	Primary         string                       `yaml:"primary,omitempty"`
	PrimaryByTarget map[string]map[string]string `yaml:"primary_by_target,omitempty"`
}

type LoadedManifest struct {
	Manifest Manifest
	Dir      string
	Path     string
	Name     string
}

func LoadManifest(dir string) (*LoadedManifest, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", dir, err)
	}

	manifestPath := filepath.Join(absDir, ManifestFileName)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", manifestPath, err)
	}

	var manifest Manifest
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parse %s: %w", manifestPath, err)
	}

	loaded := &LoadedManifest{
		Manifest: manifest,
		Dir:      absDir,
		Path:     manifestPath,
		Name:     filepath.Base(absDir),
	}

	if err := normalizeManifest(loaded); err != nil {
		return nil, err
	}
	if err := validateManifest(loaded); err != nil {
		return nil, err
	}

	return loaded, nil
}

func (m *LoadedManifest) SupportsCurrentPlatform() bool {
	return m.SupportsTarget(canonicalRuntimeTarget())
}

func (m *LoadedManifest) SupportsTarget(target string) bool {
	if m == nil || len(m.Manifest.Platforms) == 0 {
		return true
	}
	return slices.ContainsFunc(m.Manifest.Platforms, func(platform string) bool {
		return normalizePlatformName(platform) == normalizePlatformName(target)
	})
}

func (m *LoadedManifest) ResolveManifestPath(rel string) (string, error) {
	return resolveManifestPath(m.Dir, rel)
}

func (m *LoadedManifest) mustResolveManifestPath(rel string) string {
	resolved, err := m.ResolveManifestPath(rel)
	if err == nil {
		return resolved
	}
	return filepath.Join(m.Dir, filepath.FromSlash(rel))
}

func (m *LoadedManifest) BinaryPath() string {
	if binary := m.BinaryName(); binary != "" {
		return filepath.Join(m.Dir, ".op", "build", "bin", binary)
	}
	return ""
}

func (m *LoadedManifest) BinaryName() string {
	if m == nil {
		return ""
	}
	trimmed := strings.TrimSpace(m.Manifest.Artifacts.Binary)
	if trimmed == "" {
		return ""
	}
	return strings.TrimSpace(filepath.Base(trimmed))
}

// PrimaryArtifactPath returns the primary artifact path (success contract).
// Target-aware artifacts take precedence over artifacts.primary, then artifacts.binary.
func (m *LoadedManifest) PrimaryArtifactPath(ctx BuildContext) string {
	if isAggregateBuildTarget(ctx.Target) {
		return ""
	}
	if byTarget, ok := m.Manifest.Artifacts.PrimaryByTarget[ctx.Target]; ok {
		if p := strings.TrimSpace(byTarget[ctx.Mode]); p != "" {
			return m.mustResolveManifestPath(p)
		}
	}
	if p := strings.TrimSpace(m.Manifest.Artifacts.Primary); p != "" {
		return m.mustResolveManifestPath(p)
	}
	return m.BinaryPath()
}

func (m *LoadedManifest) OpRoot() string {
	return filepath.Join(m.Dir, ".op")
}

func (m *LoadedManifest) CMakeBuildDir() string {
	return filepath.Join(m.Dir, ".op", "build", "cmake")
}

func (m *LoadedManifest) GoMainPackage() string {
	if strings.TrimSpace(m.Manifest.Build.Main) != "" {
		return m.Manifest.Build.Main
	}
	return "./cmd/" + m.Name
}

func validateManifest(m *LoadedManifest) error {
	if m.Manifest.Schema != SchemaV0 {
		return fmt.Errorf("%s: schema must be %q", m.Path, SchemaV0)
	}

	switch m.Manifest.Kind {
	case KindNative, KindWrapper, KindComposite:
	default:
		return fmt.Errorf("%s: kind must be %q, %q, or %q", m.Path, KindNative, KindWrapper, KindComposite)
	}

	switch m.Manifest.Build.Runner {
	case RunnerGoModule, RunnerCMake, RunnerRecipe:
	default:
		return fmt.Errorf("%s: build.runner must be %q, %q, or %q", m.Path, RunnerGoModule, RunnerCMake, RunnerRecipe)
	}

	// Artifact validation: binary required for native/wrapper, primary or target-aware primary required for composite.
	hasBinary := strings.TrimSpace(m.Manifest.Artifacts.Binary) != ""
	hasPrimary := strings.TrimSpace(m.Manifest.Artifacts.Primary) != ""
	hasPrimaryByTarget := len(m.Manifest.Artifacts.PrimaryByTarget) > 0

	switch m.Manifest.Kind {
	case KindNative, KindWrapper:
		if !hasBinary {
			return fmt.Errorf("%s: artifacts.binary is required for %s holons", m.Path, m.Manifest.Kind)
		}
	case KindComposite:
		if !hasPrimary && !hasPrimaryByTarget {
			return fmt.Errorf("%s: artifacts.primary or artifacts.primary_by_target is required for composite holons", m.Path)
		}
	}
	if hasBinary {
		if err := validateBinaryName(m, m.Manifest.Artifacts.Binary); err != nil {
			return err
		}
	}
	if hasPrimary {
		if err := validateManifestRelativeField(m, "artifacts.primary", m.Manifest.Artifacts.Primary); err != nil {
			return err
		}
	}
	for target, byMode := range m.Manifest.Artifacts.PrimaryByTarget {
		if len(byMode) == 0 {
			return fmt.Errorf("%s: artifacts.primary_by_target[%q] must declare at least one mode", m.Path, target)
		}
		for mode, relPath := range byMode {
			if !isValidBuildMode(mode) {
				return fmt.Errorf("%s: artifacts.primary_by_target[%q] mode %q must be one of debug, release, profile", m.Path, target, mode)
			}
			if err := validateManifestRelativeField(m, fmt.Sprintf("artifacts.primary_by_target[%q][%q]", target, mode), relPath); err != nil {
				return err
			}
		}
	}

	if m.Manifest.Build.Runner != RunnerGoModule && strings.TrimSpace(m.Manifest.Build.Main) != "" {
		return fmt.Errorf("%s: build.main is only valid for %q", m.Path, RunnerGoModule)
	}

	if m.Manifest.Kind != KindWrapper && len(m.Manifest.Delegates.Commands) > 0 {
		return fmt.Errorf("%s: delegates.commands is only valid for wrapper holons", m.Path)
	}

	// Recipe-specific validation.
	if m.Manifest.Build.Runner == RunnerRecipe {
		if err := validateRecipe(m); err != nil {
			return err
		}
	}

	for _, platform := range m.Manifest.Platforms {
		if !isValidPlatform(platform) {
			return fmt.Errorf("%s: unsupported platform %q", m.Path, platform)
		}
	}

	if err := validateList("requires.commands", m.Manifest.Requires.Commands); err != nil {
		return fmt.Errorf("%s: %w", m.Path, err)
	}
	if err := validateList("requires.files", m.Manifest.Requires.Files); err != nil {
		return fmt.Errorf("%s: %w", m.Path, err)
	}
	for _, requiredFile := range m.Manifest.Requires.Files {
		if err := validateManifestRelativeField(m, "requires.files", requiredFile); err != nil {
			return err
		}
	}
	if err := validateList("delegates.commands", m.Manifest.Delegates.Commands); err != nil {
		return fmt.Errorf("%s: %w", m.Path, err)
	}

	return nil
}

// validateRecipe checks recipe-specific manifest constraints.
func validateRecipe(m *LoadedManifest) error {
	if len(m.Manifest.Build.Members) == 0 {
		return fmt.Errorf("%s: recipe runner requires at least one member", m.Path)
	}
	if len(m.Manifest.Build.Targets) == 0 {
		return fmt.Errorf("%s: recipe runner requires at least one target", m.Path)
	}

	memberIDs := make(map[string]bool, len(m.Manifest.Build.Members))
	memberTypes := make(map[string]string, len(m.Manifest.Build.Members))
	for _, member := range m.Manifest.Build.Members {
		if strings.TrimSpace(member.ID) == "" {
			return fmt.Errorf("%s: recipe member must have an id", m.Path)
		}
		if memberIDs[member.ID] {
			return fmt.Errorf("%s: duplicate recipe member id %q", m.Path, member.ID)
		}
		memberIDs[member.ID] = true

		if strings.TrimSpace(member.Path) == "" {
			return fmt.Errorf("%s: recipe member %q must have a path", m.Path, member.ID)
		}
		if err := validateManifestRelativeField(m, fmt.Sprintf("build.members[%q].path", member.ID), member.Path); err != nil {
			return err
		}
		switch member.Type {
		case "holon", "component":
		default:
			return fmt.Errorf("%s: recipe member %q type must be \"holon\" or \"component\"", m.Path, member.ID)
		}
		memberTypes[member.ID] = member.Type
	}

	if defaults := m.Manifest.Build.Defaults; defaults != nil && defaults.Target != "" {
		if _, ok := m.Manifest.Build.Targets[defaults.Target]; !ok {
			return fmt.Errorf("%s: recipe default target %q is not defined in build.targets", m.Path, defaults.Target)
		}
	}

	for targetName, target := range m.Manifest.Build.Targets {
		if len(target.Steps) == 0 {
			return fmt.Errorf("%s: target %q must declare at least one step", m.Path, targetName)
		}
		for i, step := range target.Steps {
			if step.actionCount() != 1 {
				return fmt.Errorf("%s: target %q step %d must declare exactly one action", m.Path, targetName, i+1)
			}
			if step.BuildMember != "" {
				if !memberIDs[step.BuildMember] {
					return fmt.Errorf("%s: target %q step %d references unknown member %q", m.Path, targetName, i+1, step.BuildMember)
				}
				if memberTypes[step.BuildMember] != "holon" {
					return fmt.Errorf("%s: target %q step %d build_member %q must reference a holon member", m.Path, targetName, i+1, step.BuildMember)
				}
			}
			if step.Exec != nil {
				if len(step.Exec.Argv) == 0 {
					return fmt.Errorf("%s: target %q step %d exec.argv must not be empty", m.Path, targetName, i+1)
				}
				if err := validateManifestRelativeField(m, fmt.Sprintf("build.targets[%q].steps[%d].exec.cwd", targetName, i+1), step.Exec.Cwd); err != nil {
					return err
				}
			}
			if step.Copy != nil {
				if err := validateManifestRelativeField(m, fmt.Sprintf("build.targets[%q].steps[%d].copy.from", targetName, i+1), step.Copy.From); err != nil {
					return err
				}
				if err := validateManifestRelativeField(m, fmt.Sprintf("build.targets[%q].steps[%d].copy.to", targetName, i+1), step.Copy.To); err != nil {
					return err
				}
			}
			if step.AssertFile != nil {
				if err := validateManifestRelativeField(m, fmt.Sprintf("build.targets[%q].steps[%d].assert_file.path", targetName, i+1), step.AssertFile.Path); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func validateList(field string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return fmt.Errorf("%s cannot contain empty values", field)
		}
		if _, ok := seen[trimmed]; ok {
			return fmt.Errorf("%s contains duplicate value %q", field, trimmed)
		}
		seen[trimmed] = struct{}{}
	}
	return nil
}

func normalizeManifest(m *LoadedManifest) error {
	normalizedPlatforms := make([]string, 0, len(m.Manifest.Platforms))
	for _, platform := range m.Manifest.Platforms {
		normalizedPlatforms = append(normalizedPlatforms, normalizePlatformName(platform))
	}
	m.Manifest.Platforms = normalizedPlatforms

	if defaults := m.Manifest.Build.Defaults; defaults != nil && defaults.Target != "" {
		target, err := normalizeBuildTarget(defaults.Target)
		if err != nil {
			return fmt.Errorf("%s: build.defaults.target: %w", m.Path, err)
		}
		defaults.Target = target
	}
	if defaults := m.Manifest.Build.Defaults; defaults != nil && defaults.Mode != "" {
		defaults.Mode = normalizeBuildMode(defaults.Mode)
		if !isValidBuildMode(defaults.Mode) {
			return fmt.Errorf("%s: build.defaults.mode %q must be one of debug, release, profile", m.Path, defaults.Mode)
		}
	}

	if len(m.Manifest.Build.Targets) > 0 {
		normalizedTargets := make(map[string]RecipeTarget, len(m.Manifest.Build.Targets))
		for target, recipeTarget := range m.Manifest.Build.Targets {
			normalizedTarget, err := normalizeBuildTarget(target)
			if err != nil {
				return fmt.Errorf("%s: build.targets[%q]: %w", m.Path, target, err)
			}
			if _, exists := normalizedTargets[normalizedTarget]; exists {
				return fmt.Errorf("%s: duplicate recipe target after normalization: %q", m.Path, normalizedTarget)
			}
			normalizedTargets[normalizedTarget] = recipeTarget
		}
		m.Manifest.Build.Targets = normalizedTargets
	}

	if len(m.Manifest.Artifacts.PrimaryByTarget) > 0 {
		normalizedArtifacts := make(map[string]map[string]string, len(m.Manifest.Artifacts.PrimaryByTarget))
		for target, byMode := range m.Manifest.Artifacts.PrimaryByTarget {
			normalizedTarget, err := normalizeBuildTarget(target)
			if err != nil {
				return fmt.Errorf("%s: artifacts.primary_by_target[%q]: %w", m.Path, target, err)
			}
			if _, exists := normalizedArtifacts[normalizedTarget]; exists {
				return fmt.Errorf("%s: duplicate artifacts.primary_by_target entry after normalization: %q", m.Path, normalizedTarget)
			}
			normalizedModes := make(map[string]string, len(byMode))
			for mode, relPath := range byMode {
				normalizedMode := normalizeBuildMode(mode)
				if !isValidBuildMode(normalizedMode) {
					return fmt.Errorf("%s: artifacts.primary_by_target[%q] mode %q must be one of debug, release, profile", m.Path, target, mode)
				}
				if _, exists := normalizedModes[normalizedMode]; exists {
					return fmt.Errorf("%s: duplicate artifacts.primary_by_target[%q] mode %q", m.Path, normalizedTarget, normalizedMode)
				}
				normalizedModes[normalizedMode] = relPath
			}
			normalizedArtifacts[normalizedTarget] = normalizedModes
		}
		m.Manifest.Artifacts.PrimaryByTarget = normalizedArtifacts
	}

	return nil
}

func validateManifestRelativeField(m *LoadedManifest, field, relPath string) error {
	if _, err := m.ResolveManifestPath(relPath); err != nil {
		return fmt.Errorf("%s: %s: %w", m.Path, field, err)
	}
	return nil
}

func validateBinaryName(m *LoadedManifest, name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("%s: artifacts.binary must not be empty", m.Path)
	}
	if filepath.Base(trimmed) != trimmed || strings.Contains(trimmed, "/") || strings.Contains(trimmed, `\`) {
		return fmt.Errorf("%s: artifacts.binary must be a binary name, not a path", m.Path)
	}
	if trimmed == "." || trimmed == ".." {
		return fmt.Errorf("%s: artifacts.binary must be a binary name, not %q", m.Path, trimmed)
	}
	return nil
}

func resolveManifestPath(baseDir, rel string) (string, error) {
	trimmed := strings.TrimSpace(rel)
	if trimmed == "" {
		return "", fmt.Errorf("path must not be empty")
	}
	if filepath.IsAbs(trimmed) {
		return "", fmt.Errorf("path must be relative to the manifest directory")
	}
	cleaned := filepath.Clean(filepath.FromSlash(trimmed))
	fullPath := filepath.Join(baseDir, cleaned)
	relToBase, err := filepath.Rel(baseDir, fullPath)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if relToBase == ".." || strings.HasPrefix(relToBase, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path must stay within the manifest directory")
	}
	return fullPath, nil
}

func (s RecipeStep) actionCount() int {
	count := 0
	if strings.TrimSpace(s.BuildMember) != "" {
		count++
	}
	if s.Exec != nil {
		count++
	}
	if s.Copy != nil {
		count++
	}
	if s.AssertFile != nil {
		count++
	}
	return count
}

func isValidPlatform(platform string) bool {
	switch normalizePlatformName(platform) {
	case "aix", "android", "ios", "ios-simulator", "js", "linux", "macos", "netbsd", "openbsd",
		"plan9", "solaris", "tvos", "tvos-simulator", "visionos", "visionos-simulator", "wasip1", "watchos", "watchos-simulator", "windows",
		"dragonfly", "freebsd", "illumos":
		return true
	default:
		return false
	}
}
