package holons

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

var runnerRegistry = map[string]runner{
	RunnerGoModule: goModuleRunner{},
	RunnerCMake:    cmakeRunner{},
	RunnerCargo:    cargoRunner{},
	RunnerPython:   pythonRunner{},
	RunnerDart:     dartRunner{},
	RunnerRuby:     rubyRunner{},
	RunnerSwiftPkg: swiftPackageRunner{},
	RunnerFlutter:  flutterRunner{},
	RunnerNPM:      npmRunner{},
	RunnerGradle:   gradleRunner{},
	RunnerDotnet:   dotnetRunner{},
	RunnerQtCMake:  qtCMakeRunner{},
	RunnerRecipe:   recipeRunner{},
}

func isSupportedRunner(name string) bool {
	_, ok := runnerRegistry[strings.TrimSpace(name)]
	return ok
}

func supportedRunnerList() string {
	names := make([]string, 0, len(runnerRegistry))
	for name := range runnerRegistry {
		names = append(names, fmt.Sprintf("%q", name))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func manifestHasPrimaryArtifact(manifest *LoadedManifest) bool {
	if manifest == nil {
		return false
	}
	return strings.TrimSpace(manifest.Manifest.Artifacts.Primary) != ""
}

func requireRunnerCommands(commands ...string) error {
	for _, command := range commands {
		if _, err := exec.LookPath(command); err != nil {
			return fmt.Errorf("missing required command %q on PATH; %s", command, installHint(command))
		}
	}
	return nil
}

func hostExecutableName(name string) string {
	if runtime.GOOS == "windows" && filepath.Ext(name) == "" {
		return name + ".exe"
	}
	return name
}

func syncBinaryArtifact(manifest *LoadedManifest, src string) error {
	if manifest == nil || manifestHasPrimaryArtifact(manifest) {
		return nil
	}
	if strings.TrimSpace(src) == "" {
		return fmt.Errorf("build did not produce %s", manifest.BinaryName())
	}
	if err := os.MkdirAll(filepath.Dir(manifest.BinaryPath()), 0o755); err != nil {
		return err
	}
	return copyFile(src, manifest.BinaryPath())
}

func syncBinaryFromCandidates(manifest *LoadedManifest, candidates []string) error {
	if manifest == nil || manifestHasPrimaryArtifact(manifest) {
		return nil
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return syncBinaryArtifact(manifest, candidate)
		}
	}
	trimmed := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate != "" {
			trimmed = append(trimmed, workspaceRelativePath(candidate))
		}
	}
	return fmt.Errorf("build did not produce %s (searched: %s)", manifest.BinaryName(), strings.Join(trimmed, ", "))
}

func hasCMakeProject(manifest *LoadedManifest) bool {
	if manifest == nil {
		return false
	}
	info, err := os.Stat(filepath.Join(manifest.Dir, "CMakeLists.txt"))
	return err == nil && !info.IsDir()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func firstAvailableCommand(candidates ...string) (string, error) {
	for _, candidate := range candidates {
		if _, err := exec.LookPath(candidate); err == nil {
			return candidate, nil
		}
	}
	quoted := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		quoted = append(quoted, fmt.Sprintf("%q", candidate))
	}
	return "", fmt.Errorf("missing required command on PATH; expected one of %s", strings.Join(quoted, ", "))
}

func pythonInterpreter() (string, error) {
	interpreter, err := firstAvailableCommand("python3", "python")
	if err != nil {
		return "", fmt.Errorf("python runner requires python3 or python on PATH")
	}
	return interpreter, nil
}

func pythonBuildArgs(manifest *LoadedManifest) ([]string, bool, error) {
	interpreter, err := pythonInterpreter()
	if err != nil {
		return nil, false, err
	}
	if !fileExists(filepath.Join(manifest.Dir, "requirements.txt")) {
		return nil, false, nil
	}
	return []string{interpreter, "-m", "pip", "install", "-r", "requirements.txt"}, true, nil
}

func pythonTestArgs(manifest *LoadedManifest) ([]string, error) {
	interpreter, err := pythonInterpreter()
	if err != nil {
		return nil, err
	}
	if dirExists(filepath.Join(manifest.Dir, "tests")) {
		return []string{interpreter, "-m", "unittest", "discover"}, nil
	}
	if _, err := exec.LookPath("pytest"); err == nil {
		return []string{"pytest"}, nil
	}
	return nil, fmt.Errorf("python runner requires tests/ or pytest on PATH")
}

func dartEntrypoint(manifest *LoadedManifest) (string, error) {
	for _, rel := range []string{"bin/main.dart", "lib/main.dart"} {
		if fileExists(filepath.Join(manifest.Dir, filepath.FromSlash(rel))) {
			return rel, nil
		}
	}
	return "", fmt.Errorf("dart runner requires bin/main.dart or lib/main.dart")
}

func rubyTestArgs(manifest *LoadedManifest) ([]string, error) {
	if dirExists(filepath.Join(manifest.Dir, "spec")) {
		return []string{"bundle", "exec", "rspec"}, nil
	}
	if fileExists(filepath.Join(manifest.Dir, "Rakefile")) {
		return []string{"bundle", "exec", "rake", "test"}, nil
	}
	return nil, fmt.Errorf("ruby runner requires spec/ or Rakefile")
}

func removeSelectedPaths(root string, relPaths ...string) error {
	for _, relPath := range relPaths {
		if err := os.RemoveAll(filepath.Join(root, relPath)); err != nil {
			return err
		}
	}
	return nil
}

func removeNamedDirs(root string, names ...string) error {
	nameSet := make(map[string]struct{}, len(names))
	for _, name := range names {
		nameSet[name] = struct{}{}
	}

	var matches []string
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if _, ok := nameSet[d.Name()]; ok {
			matches = append(matches, path)
			return filepath.SkipDir
		}
		return nil
	}); err != nil {
		return err
	}

	sort.Slice(matches, func(i, j int) bool { return len(matches[i]) > len(matches[j]) })
	for _, match := range matches {
		if err := os.RemoveAll(match); err != nil {
			return err
		}
	}
	return nil
}

type cargoRunner struct{}

func (cargoRunner) check(manifest *LoadedManifest, _ BuildContext) error {
	if err := requireRunnerCommands("cargo", "rustc"); err != nil {
		return err
	}
	if hasCMakeProject(manifest) {
		return requireRunnerCommands("cmake")
	}
	return nil
}

func (cargoRunner) build(manifest *LoadedManifest, ctx BuildContext, report *Report) error {
	if hasCMakeProject(manifest) {
		return cmakeRunner{}.build(manifest, ctx, report)
	}
	if err := ensureHostBuildTarget(RunnerCargo, ctx); err != nil {
		return err
	}

	targetDir := filepath.Join(manifest.OpRoot(), "build", "cargo")
	args := []string{"cargo", "build", "--target-dir", targetDir}
	if normalizeBuildMode(ctx.Mode) != buildModeDebug {
		args = append(args, "--release")
	}
	report.Commands = append(report.Commands, commandString(args))
	ctx.Progress.Step(commandString(args))
	if ctx.DryRun {
		return nil
	}
	if output, err := runCommand(manifest.Dir, args); err != nil {
		return fmt.Errorf("%s\n%s", err, output)
	}
	source := filepath.Join(targetDir, cargoModeDir(ctx.Mode), hostExecutableName(manifest.BinaryName()))
	if err := syncBinaryArtifact(manifest, source); err != nil {
		return err
	}
	report.Notes = append(report.Notes, "cargo build complete")
	return nil
}

func (cargoRunner) test(manifest *LoadedManifest, ctx BuildContext, report *Report) error {
	args := []string{"cargo", "test", "--target-dir", filepath.Join(manifest.OpRoot(), "build", "cargo")}
	report.Commands = append(report.Commands, commandString(args))
	ctx.Progress.Step(commandString(args))
	if output, err := runCommand(manifest.Dir, args); err != nil {
		return fmt.Errorf("%s\n%s", err, output)
	}
	report.Notes = append(report.Notes, "cargo test passed")
	return nil
}

func (cargoRunner) clean(manifest *LoadedManifest, report *Report) error {
	if err := os.RemoveAll(manifest.OpRoot()); err != nil {
		return err
	}
	report.Notes = append(report.Notes, "removed .op/")
	return nil
}

func cargoModeDir(mode string) string {
	if normalizeBuildMode(mode) == buildModeDebug {
		return "debug"
	}
	return "release"
}

type pythonRunner struct{}

func (pythonRunner) check(_ *LoadedManifest, _ BuildContext) error {
	_, err := pythonInterpreter()
	return err
}

func (pythonRunner) build(manifest *LoadedManifest, ctx BuildContext, report *Report) error {
	if err := ensureHostBuildTarget(RunnerPython, ctx); err != nil {
		return err
	}

	args, ok, err := pythonBuildArgs(manifest)
	if err != nil {
		return err
	}
	if !ok {
		report.Notes = append(report.Notes, "no requirements.txt; skipping dependency install")
		return nil
	}
	report.Commands = append(report.Commands, commandString(args))
	ctx.Progress.Step(commandString(args))
	if ctx.DryRun {
		return nil
	}
	if output, err := runCommand(manifest.Dir, args); err != nil {
		return fmt.Errorf("%s\n%s", err, output)
	}
	report.Notes = append(report.Notes, "python dependencies installed")
	return nil
}

func (pythonRunner) test(manifest *LoadedManifest, ctx BuildContext, report *Report) error {
	if err := ensureHostBuildTarget(RunnerPython, ctx); err != nil {
		return err
	}

	args, err := pythonTestArgs(manifest)
	if err != nil {
		return err
	}
	report.Commands = append(report.Commands, commandString(args))
	ctx.Progress.Step(commandString(args))
	if output, err := runCommand(manifest.Dir, args); err != nil {
		return fmt.Errorf("%s\n%s", err, output)
	}
	report.Notes = append(report.Notes, "python tests passed")
	return nil
}

func (pythonRunner) clean(manifest *LoadedManifest, report *Report) error {
	if err := removeNamedDirs(manifest.Dir, "__pycache__"); err != nil {
		return err
	}
	if err := removeSelectedPaths(manifest.Dir, ".pytest_cache", "build", "dist"); err != nil {
		return err
	}
	if err := os.RemoveAll(manifest.OpRoot()); err != nil {
		return err
	}
	report.Notes = append(report.Notes, "removed Python caches, build/, dist/, and .op/")
	return nil
}

type dartRunner struct{}

func (dartRunner) check(manifest *LoadedManifest, _ BuildContext) error {
	if err := requireRunnerCommands("dart"); err != nil {
		return err
	}
	if !fileExists(filepath.Join(manifest.Dir, "pubspec.yaml")) {
		return fmt.Errorf("dart runner requires pubspec.yaml")
	}
	_, err := dartEntrypoint(manifest)
	return err
}

func (dartRunner) build(manifest *LoadedManifest, ctx BuildContext, report *Report) error {
	if err := ensureHostBuildTarget(RunnerDart, ctx); err != nil {
		return err
	}

	entrypoint, err := dartEntrypoint(manifest)
	if err != nil {
		return err
	}
	outputPath := manifest.ArtifactPath(ctx)
	if strings.TrimSpace(outputPath) == "" {
		return fmt.Errorf("dart runner requires an artifact output path")
	}

	commands := [][]string{
		{"dart", "pub", "get"},
		{"dart", "compile", "exe", filepath.FromSlash(entrypoint), "-o", outputPath},
	}
	for _, args := range commands {
		report.Commands = append(report.Commands, commandString(args))
		ctx.Progress.Step(commandString(args))
	}
	if ctx.DryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	for _, args := range commands {
		if output, err := runCommand(manifest.Dir, args); err != nil {
			return fmt.Errorf("%s\n%s", err, output)
		}
	}
	report.Notes = append(report.Notes, "dart build complete")
	return nil
}

func (dartRunner) test(manifest *LoadedManifest, ctx BuildContext, report *Report) error {
	if err := ensureHostBuildTarget(RunnerDart, ctx); err != nil {
		return err
	}

	args := []string{"dart", "test"}
	report.Commands = append(report.Commands, commandString(args))
	ctx.Progress.Step(commandString(args))
	if output, err := runCommand(manifest.Dir, args); err != nil {
		return fmt.Errorf("%s\n%s", err, output)
	}
	report.Notes = append(report.Notes, "dart test passed")
	return nil
}

func (dartRunner) clean(manifest *LoadedManifest, report *Report) error {
	if err := removeSelectedPaths(manifest.Dir, "build", ".dart_tool"); err != nil {
		return err
	}
	if err := os.RemoveAll(manifest.OpRoot()); err != nil {
		return err
	}
	report.Notes = append(report.Notes, "removed build/, .dart_tool/, and .op/")
	return nil
}

type rubyRunner struct{}

func (rubyRunner) check(manifest *LoadedManifest, _ BuildContext) error {
	if err := requireRunnerCommands("ruby", "bundle"); err != nil {
		return err
	}
	if !fileExists(filepath.Join(manifest.Dir, "Gemfile")) {
		return fmt.Errorf("ruby runner requires Gemfile")
	}
	return nil
}

func (rubyRunner) build(manifest *LoadedManifest, ctx BuildContext, report *Report) error {
	if err := ensureHostBuildTarget(RunnerRuby, ctx); err != nil {
		return err
	}

	args := []string{"bundle", "install"}
	report.Commands = append(report.Commands, commandString(args))
	ctx.Progress.Step(commandString(args))
	if ctx.DryRun {
		return nil
	}
	if output, err := runCommand(manifest.Dir, args); err != nil {
		return fmt.Errorf("%s\n%s", err, output)
	}
	report.Notes = append(report.Notes, "bundle install complete")
	return nil
}

func (rubyRunner) test(manifest *LoadedManifest, ctx BuildContext, report *Report) error {
	if err := ensureHostBuildTarget(RunnerRuby, ctx); err != nil {
		return err
	}

	args, err := rubyTestArgs(manifest)
	if err != nil {
		return err
	}
	report.Commands = append(report.Commands, commandString(args))
	ctx.Progress.Step(commandString(args))
	if output, err := runCommand(manifest.Dir, args); err != nil {
		return fmt.Errorf("%s\n%s", err, output)
	}
	report.Notes = append(report.Notes, "ruby tests passed")
	return nil
}

func (rubyRunner) clean(manifest *LoadedManifest, report *Report) error {
	if err := removeSelectedPaths(manifest.Dir, "log", "tmp", filepath.Join("vendor", "bundle")); err != nil {
		return err
	}
	if err := os.RemoveAll(manifest.OpRoot()); err != nil {
		return err
	}
	report.Notes = append(report.Notes, "removed log/, tmp/, vendor/bundle/, and .op/")
	return nil
}

type swiftPackageRunner struct{}

func (swiftPackageRunner) check(manifest *LoadedManifest, _ BuildContext) error {
	if err := requireRunnerCommands("swift", "xcodebuild"); err != nil {
		return err
	}
	if hasSwiftPackage(manifest) {
		return nil
	}
	if _, _, ok := detectXcodeContainer(manifest); ok {
		return nil
	}
	return fmt.Errorf("swift-package runner requires Package.swift, .xcodeproj, or .xcworkspace")
}

func (swiftPackageRunner) build(manifest *LoadedManifest, ctx BuildContext, report *Report) error {
	if err := ensureHostBuildTarget(RunnerSwiftPkg, ctx); err != nil {
		return err
	}

	if hasSwiftPackage(manifest) {
		buildPath := filepath.Join(manifest.OpRoot(), "build", "swift")
		args := []string{"swift", "build", "--build-path", buildPath, "-c", swiftBuildMode(ctx.Mode)}
		report.Commands = append(report.Commands, commandString(args))
		ctx.Progress.Step(commandString(args))
		if ctx.DryRun {
			return nil
		}
		if output, err := runCommand(manifest.Dir, args); err != nil {
			return fmt.Errorf("%s\n%s", err, output)
		}
		source := filepath.Join(buildPath, swiftBuildMode(ctx.Mode), hostExecutableName(manifest.BinaryName()))
		if err := syncBinaryArtifact(manifest, source); err != nil {
			return err
		}
		report.Notes = append(report.Notes, "swift build complete")
		return nil
	}

	flag, container, _ := detectXcodeContainer(manifest)
	symroot := filepath.Join(manifest.OpRoot(), "build", "xcode")
	args := []string{
		"xcodebuild",
		flag, container,
		"-scheme", manifest.BinaryName(),
		"-configuration", cmakeBuildConfig(ctx.Mode),
		"SYMROOT=" + symroot,
		"build",
	}
	report.Commands = append(report.Commands, commandString(args))
	ctx.Progress.Step(commandString(args))
	if ctx.DryRun {
		return nil
	}
	if output, err := runCommand(manifest.Dir, args); err != nil {
		return fmt.Errorf("%s\n%s", err, output)
	}
	source := filepath.Join(symroot, "Build", "Products", cmakeBuildConfig(ctx.Mode), hostExecutableName(manifest.BinaryName()))
	if err := syncBinaryArtifact(manifest, source); err != nil {
		return err
	}
	report.Notes = append(report.Notes, "xcode build complete")
	return nil
}

func (swiftPackageRunner) test(manifest *LoadedManifest, ctx BuildContext, report *Report) error {
	if hasSwiftPackage(manifest) {
		args := []string{"swift", "test", "--build-path", filepath.Join(manifest.OpRoot(), "build", "swift"), "-c", swiftBuildMode(ctx.Mode)}
		report.Commands = append(report.Commands, commandString(args))
		ctx.Progress.Step(commandString(args))
		if output, err := runCommand(manifest.Dir, args); err != nil {
			return fmt.Errorf("%s\n%s", err, output)
		}
		report.Notes = append(report.Notes, "swift test passed")
		return nil
	}
	return fmt.Errorf("swift-package test is only supported for Package.swift projects")
}

func (swiftPackageRunner) clean(manifest *LoadedManifest, report *Report) error {
	if hasSwiftPackage(manifest) {
		args := []string{"swift", "package", "clean"}
		report.Commands = append(report.Commands, commandString(args))
		if _, err := exec.LookPath("swift"); err == nil {
			if output, cmdErr := runCommand(manifest.Dir, args); cmdErr != nil {
				return fmt.Errorf("%s\n%s", cmdErr, output)
			}
		}
	}
	if err := os.RemoveAll(manifest.OpRoot()); err != nil {
		return err
	}
	report.Notes = append(report.Notes, "removed .op/")
	return nil
}

func hasSwiftPackage(manifest *LoadedManifest) bool {
	info, err := os.Stat(filepath.Join(manifest.Dir, "Package.swift"))
	return err == nil && !info.IsDir()
}

func detectXcodeContainer(manifest *LoadedManifest) (string, string, bool) {
	workspaceMatches, _ := filepath.Glob(filepath.Join(manifest.Dir, "*.xcworkspace"))
	if len(workspaceMatches) > 0 {
		return "-workspace", filepath.Base(workspaceMatches[0]), true
	}
	projectMatches, _ := filepath.Glob(filepath.Join(manifest.Dir, "*.xcodeproj"))
	if len(projectMatches) > 0 {
		return "-project", filepath.Base(projectMatches[0]), true
	}
	return "", "", false
}

func swiftBuildMode(mode string) string {
	if normalizeBuildMode(mode) == buildModeDebug {
		return "debug"
	}
	return "release"
}

type flutterRunner struct{}

func (flutterRunner) check(_ *LoadedManifest, _ BuildContext) error {
	return requireRunnerCommands("flutter", "dart")
}

func (flutterRunner) build(manifest *LoadedManifest, ctx BuildContext, report *Report) error {
	args, err := flutterBuildArgs(ctx)
	if err != nil {
		return err
	}
	report.Commands = append(report.Commands, commandString(args))
	ctx.Progress.Step(commandString(args))
	if ctx.DryRun {
		return nil
	}
	if output, err := runCommand(manifest.Dir, args); err != nil {
		return fmt.Errorf("%s\n%s", err, output)
	}
	report.Notes = append(report.Notes, "flutter build complete")
	return nil
}

func (flutterRunner) test(manifest *LoadedManifest, ctx BuildContext, report *Report) error {
	args := []string{"flutter", "test"}
	report.Commands = append(report.Commands, commandString(args))
	ctx.Progress.Step(commandString(args))
	if output, err := runCommand(manifest.Dir, args); err != nil {
		return fmt.Errorf("%s\n%s", err, output)
	}
	report.Notes = append(report.Notes, "flutter test passed")
	return nil
}

func (flutterRunner) clean(manifest *LoadedManifest, report *Report) error {
	args := []string{"flutter", "clean"}
	report.Commands = append(report.Commands, commandString(args))
	if _, err := exec.LookPath("flutter"); err == nil {
		if output, cmdErr := runCommand(manifest.Dir, args); cmdErr != nil {
			return fmt.Errorf("%s\n%s", cmdErr, output)
		}
	}
	if err := os.RemoveAll(manifest.OpRoot()); err != nil {
		return err
	}
	report.Notes = append(report.Notes, "removed .op/")
	return nil
}

func flutterBuildArgs(ctx BuildContext) ([]string, error) {
	target := normalizePlatformName(ctx.Target)
	modeFlag := "--debug"
	switch normalizeBuildMode(ctx.Mode) {
	case buildModeRelease:
		modeFlag = "--release"
	case buildModeProfile:
		modeFlag = "--profile"
	}
	switch target {
	case "macos", "linux", "windows":
		return []string{"flutter", "build", target, modeFlag}, nil
	case "ios":
		return []string{"flutter", "build", "ios", modeFlag, "--no-codesign"}, nil
	case "android":
		return []string{"flutter", "build", "apk", modeFlag}, nil
	default:
		return nil, fmt.Errorf("flutter runner does not support target %q", ctx.Target)
	}
}

type npmRunner struct{}

func (npmRunner) check(manifest *LoadedManifest, _ BuildContext) error {
	if err := requireRunnerCommands("node", "npm"); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(manifest.Dir, "package.json")); err != nil {
		return fmt.Errorf("npm runner requires package.json")
	}
	return nil
}

func (npmRunner) build(manifest *LoadedManifest, ctx BuildContext, report *Report) error {
	commands := [][]string{
		{"npm", "ci"},
		{"npm", "run", "build"},
	}
	for _, args := range commands {
		report.Commands = append(report.Commands, commandString(args))
		ctx.Progress.Step(commandString(args))
	}
	if ctx.DryRun {
		return nil
	}
	for _, args := range commands {
		if output, err := runCommand(manifest.Dir, args); err != nil {
			return fmt.Errorf("%s\n%s", err, output)
		}
	}
	if err := syncBinaryFromCandidates(manifest, npmArtifactCandidates(manifest)); err != nil {
		return err
	}
	report.Notes = append(report.Notes, "npm build complete")
	return nil
}

func (npmRunner) test(manifest *LoadedManifest, ctx BuildContext, report *Report) error {
	args := []string{"npm", "test"}
	report.Commands = append(report.Commands, commandString(args))
	ctx.Progress.Step(commandString(args))
	if output, err := runCommand(manifest.Dir, args); err != nil {
		return fmt.Errorf("%s\n%s", err, output)
	}
	report.Notes = append(report.Notes, "npm test passed")
	return nil
}

func (npmRunner) clean(manifest *LoadedManifest, report *Report) error {
	for _, dir := range []string{"node_modules", "dist", "build"} {
		if err := os.RemoveAll(filepath.Join(manifest.Dir, dir)); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(manifest.OpRoot()); err != nil {
		return err
	}
	report.Notes = append(report.Notes, "removed node_modules/, dist/, build/, and .op/")
	return nil
}

func npmArtifactCandidates(manifest *LoadedManifest) []string {
	name := manifest.BinaryName()
	return []string{
		filepath.Join(manifest.Dir, "dist", name),
		filepath.Join(manifest.Dir, "dist", name+".js"),
		filepath.Join(manifest.Dir, "build", name),
		filepath.Join(manifest.Dir, "build", name+".js"),
	}
}

type gradleRunner struct{}

func (gradleRunner) check(manifest *LoadedManifest, _ BuildContext) error {
	if err := requireRunnerCommands("java"); err != nil {
		return err
	}
	if _, err := gradleInvoker(manifest); err != nil {
		return err
	}
	return nil
}

func (gradleRunner) build(manifest *LoadedManifest, ctx BuildContext, report *Report) error {
	invoker, err := gradleInvoker(manifest)
	if err != nil {
		return err
	}
	args := append(invoker, "build")
	report.Commands = append(report.Commands, commandString(args))
	ctx.Progress.Step(commandString(args))
	if ctx.DryRun {
		return nil
	}
	if output, err := runCommand(manifest.Dir, args); err != nil {
		return fmt.Errorf("%s\n%s", err, output)
	}
	if err := syncBinaryFromCandidates(manifest, gradleArtifactCandidates(manifest)); err != nil {
		return err
	}
	report.Notes = append(report.Notes, "gradle build complete")
	return nil
}

func (gradleRunner) test(manifest *LoadedManifest, ctx BuildContext, report *Report) error {
	invoker, err := gradleInvoker(manifest)
	if err != nil {
		return err
	}
	args := append(invoker, "test")
	report.Commands = append(report.Commands, commandString(args))
	ctx.Progress.Step(commandString(args))
	if output, err := runCommand(manifest.Dir, args); err != nil {
		return fmt.Errorf("%s\n%s", err, output)
	}
	report.Notes = append(report.Notes, "gradle test passed")
	return nil
}

func (gradleRunner) clean(manifest *LoadedManifest, report *Report) error {
	invoker, err := gradleInvoker(manifest)
	if err == nil {
		args := append(invoker, "clean")
		report.Commands = append(report.Commands, commandString(args))
		if output, cmdErr := runCommand(manifest.Dir, args); cmdErr != nil {
			return fmt.Errorf("%s\n%s", cmdErr, output)
		}
	}
	if err := os.RemoveAll(manifest.OpRoot()); err != nil {
		return err
	}
	report.Notes = append(report.Notes, "removed .op/")
	return nil
}

func gradleInvoker(manifest *LoadedManifest) ([]string, error) {
	for _, wrapper := range []string{"gradlew", "gradlew.bat"} {
		path := filepath.Join(manifest.Dir, wrapper)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			if runtime.GOOS == "windows" && strings.HasSuffix(wrapper, ".bat") {
				return []string{"cmd", "/c", wrapper}, nil
			}
			return []string{"./" + wrapper}, nil
		}
	}
	if _, err := exec.LookPath("gradle"); err == nil {
		return []string{"gradle"}, nil
	}
	return nil, fmt.Errorf("gradle runner requires gradlew or gradle on PATH")
}

func gradleArtifactCandidates(manifest *LoadedManifest) []string {
	name := manifest.BinaryName()
	return []string{
		filepath.Join(manifest.Dir, "build", "install", name, "bin", hostExecutableName(name)),
		filepath.Join(manifest.Dir, "build", "bin", hostExecutableName(name)),
		filepath.Join(manifest.Dir, "build", "compose", "binaries", "main", "app", name, hostExecutableName(name)),
	}
}

type dotnetRunner struct{}

func (dotnetRunner) check(manifest *LoadedManifest, ctx BuildContext) error {
	if err := requireRunnerCommands("dotnet"); err != nil {
		return err
	}
	csproj, err := dotnetProjectFile(manifest)
	if err != nil {
		return err
	}
	workload := requiredDotnetWorkload(csproj, ctx.Target)
	if workload == "" {
		return nil
	}
	output, err := runCommand(manifest.Dir, []string{"dotnet", "workload", "list"})
	if err != nil {
		return fmt.Errorf("dotnet workload list failed: %w", err)
	}
	if !strings.Contains(output, workload) {
		return fmt.Errorf("dotnet runner requires workload %q\n  install with: dotnet workload install %s", workload, workload)
	}
	return nil
}

func (dotnetRunner) build(manifest *LoadedManifest, ctx BuildContext, report *Report) error {
	outputDir := filepath.Join(manifest.OpRoot(), "build", "dotnet")
	args := []string{"dotnet", "build", "-c", cmakeBuildConfig(ctx.Mode), "-o", outputDir}
	report.Commands = append(report.Commands, commandString(args))
	ctx.Progress.Step(commandString(args))
	if ctx.DryRun {
		return nil
	}
	if output, err := runCommand(manifest.Dir, args); err != nil {
		return fmt.Errorf("%s\n%s", err, output)
	}
	if err := syncBinaryFromCandidates(manifest, []string{
		filepath.Join(outputDir, hostExecutableName(manifest.BinaryName())),
		filepath.Join(outputDir, manifest.BinaryName()+".dll"),
	}); err != nil {
		return err
	}
	report.Notes = append(report.Notes, "dotnet build complete")
	return nil
}

func (dotnetRunner) test(manifest *LoadedManifest, ctx BuildContext, report *Report) error {
	args := []string{"dotnet", "test", "-c", cmakeBuildConfig(ctx.Mode)}
	report.Commands = append(report.Commands, commandString(args))
	ctx.Progress.Step(commandString(args))
	if output, err := runCommand(manifest.Dir, args); err != nil {
		return fmt.Errorf("%s\n%s", err, output)
	}
	report.Notes = append(report.Notes, "dotnet test passed")
	return nil
}

func (dotnetRunner) clean(manifest *LoadedManifest, report *Report) error {
	args := []string{"dotnet", "clean"}
	report.Commands = append(report.Commands, commandString(args))
	if _, err := exec.LookPath("dotnet"); err == nil {
		if output, cmdErr := runCommand(manifest.Dir, args); cmdErr != nil {
			return fmt.Errorf("%s\n%s", cmdErr, output)
		}
	}
	if err := os.RemoveAll(manifest.OpRoot()); err != nil {
		return err
	}
	report.Notes = append(report.Notes, "removed .op/")
	return nil
}

func dotnetProjectFile(manifest *LoadedManifest) (string, error) {
	matches, err := filepath.Glob(filepath.Join(manifest.Dir, "*.csproj"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("dotnet runner requires a .csproj file")
	}
	sort.Strings(matches)
	return matches[0], nil
}

func requiredDotnetWorkload(projectFile, target string) string {
	data, err := os.ReadFile(projectFile)
	if err != nil {
		return ""
	}
	content := string(data)
	if !strings.Contains(content, "UseMaui") && !strings.Contains(content, "Microsoft.Maui") {
		return ""
	}
	switch normalizePlatformName(target) {
	case "macos":
		return "maui-maccatalyst"
	case "ios":
		return "maui-ios"
	case "android":
		return "maui-android"
	default:
		return ""
	}
}

type qtCMakeRunner struct{}

func (qtCMakeRunner) check(_ *LoadedManifest, _ BuildContext) error {
	if err := requireRunnerCommands("cmake"); err != nil {
		return err
	}
	_, err := detectQt6Dir()
	return err
}

func (qtCMakeRunner) build(manifest *LoadedManifest, ctx BuildContext, report *Report) error {
	if err := ensureHostBuildTarget(RunnerQtCMake, ctx); err != nil {
		return err
	}
	qtDir, err := detectQt6Dir()
	if err != nil {
		return err
	}
	config := cmakeBuildConfig(ctx.Mode)
	binDir := filepath.Join(manifest.Dir, ".op", "build", "bin")
	configureArgs := []string{
		"cmake",
		"-S", ".",
		"-B", manifest.CMakeBuildDir(),
		"-DCMAKE_BUILD_TYPE=" + config,
		"-DCMAKE_PREFIX_PATH=" + qtDir,
		"-DCMAKE_RUNTIME_OUTPUT_DIRECTORY=" + binDir,
		"-DCMAKE_RUNTIME_OUTPUT_DIRECTORY_DEBUG=" + binDir,
		"-DCMAKE_RUNTIME_OUTPUT_DIRECTORY_RELEASE=" + binDir,
		"-DCMAKE_RUNTIME_OUTPUT_DIRECTORY_RELWITHDEBINFO=" + binDir,
	}
	buildArgs := []string{"cmake", "--build", manifest.CMakeBuildDir(), "--config", config}
	report.Commands = append(report.Commands, commandString(configureArgs), commandString(buildArgs))
	if ctx.DryRun {
		return nil
	}
	if err := os.MkdirAll(manifest.CMakeBuildDir(), 0o755); err != nil {
		return err
	}
	ctx.Progress.Step(commandString(configureArgs))
	if output, err := runCommand(manifest.Dir, configureArgs); err != nil {
		return fmt.Errorf("%s\n%s", err, output)
	}
	ctx.Progress.Step(commandString(buildArgs))
	if output, err := runCommand(manifest.Dir, buildArgs); err != nil {
		return fmt.Errorf("%s\n%s", err, output)
	}
	report.Notes = append(report.Notes, "qt-cmake build complete")
	return nil
}

func (qtCMakeRunner) test(manifest *LoadedManifest, ctx BuildContext, report *Report) error {
	runner := qtCMakeRunner{}
	if err := runner.build(manifest, ctx, report); err != nil {
		return err
	}
	config := cmakeBuildConfig(ctx.Mode)
	testArgs := []string{"ctest", "--test-dir", manifest.CMakeBuildDir(), "--output-on-failure", "-C", config}
	report.Commands = append(report.Commands, commandString(testArgs))
	ctx.Progress.Step(commandString(testArgs))
	if output, err := runCommand(manifest.Dir, testArgs); err != nil {
		return fmt.Errorf("%s\n%s", err, output)
	}
	report.Notes = append(report.Notes, "ctest passed")
	return nil
}

func (qtCMakeRunner) clean(manifest *LoadedManifest, report *Report) error {
	if err := os.RemoveAll(manifest.OpRoot()); err != nil {
		return err
	}
	report.Notes = append(report.Notes, "removed .op/")
	return nil
}

func detectQt6Dir() (string, error) {
	if qtDir := strings.TrimSpace(os.Getenv("Qt6_DIR")); qtDir != "" {
		if _, err := os.Stat(qtDir); err == nil {
			return qtDir, nil
		}
	}
	if runtime.GOOS == "darwin" {
		if _, err := exec.LookPath("brew"); err == nil {
			output, cmdErr := runCommand(".", []string{"brew", "--prefix", "qt6"})
			if cmdErr == nil {
				prefix := strings.TrimSpace(output)
				if prefix != "" {
					qtDir := filepath.Join(prefix, "lib", "cmake", "Qt6")
					if _, err := os.Stat(qtDir); err == nil {
						return qtDir, nil
					}
				}
			}
		}
	}
	return "", fmt.Errorf("qt-cmake runner requires Qt6\n  install with: brew install qt6\n  then set: export Qt6_DIR=$(brew --prefix qt6)/lib/cmake/Qt6")
}
