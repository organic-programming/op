package holons

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/organic-programming/sophia-who/pkg/identity"
)

func TestLoadManifestRejectsUnknownField(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ManifestFileName), []byte("schema: holon/v0\nkind: native\nunknown: true\nbuild:\n  runner: go-module\nartifacts:\n  binary: .op/build/bin/demo\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadManifest(root)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "field unknown not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveTargetByAliasAcrossRoots(t *testing.T) {
	root := t.TempDir()
	chdirForHolonTest(t, root)

	dir := filepath.Join(root, "organic-programming", "holons", "sophia-who")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	id := identity.Identity{
		UUID:        "1234",
		GivenName:   "Sophia",
		FamilyName:  "Who?",
		Motto:       "Know thyself.",
		Composer:    "test",
		Clade:       "deterministic/pure",
		Status:      "draft",
		Born:        "2026-03-06",
		Aliases:     []string{"who"},
		GeneratedBy: "test",
		Lang:        "go",
	}
	if err := identity.WriteHolonMD(id, filepath.Join(dir, "HOLON.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestFileName), []byte("schema: holon/v0\nkind: native\nbuild:\n  runner: go-module\n  main: ./cmd/who\nrequires:\n  commands: [go]\n  files: [go.mod]\nartifacts:\n  binary: .op/build/bin/sophia-who\n"), 0644); err != nil {
		t.Fatal(err)
	}

	target, err := ResolveTarget("who")
	if err != nil {
		t.Fatalf("ResolveTarget returned error: %v", err)
	}
	if got := filepath.Base(target.Dir); got != "sophia-who" {
		t.Fatalf("dir basename = %q, want %q", got, "sophia-who")
	}
}

func TestResolveBinaryUsesCanonicalArtifactNameForAlias(t *testing.T) {
	root := t.TempDir()
	chdirForHolonTest(t, root)

	dir := filepath.Join(root, "organic-programming", "holons", "sophia-who")
	if err := os.MkdirAll(filepath.Join(dir, ".op", "build", "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	id := identity.Identity{
		UUID:        "5678",
		GivenName:   "Sophia",
		FamilyName:  "Who?",
		Motto:       "Know thyself.",
		Composer:    "test",
		Clade:       "deterministic/pure",
		Status:      "draft",
		Born:        "2026-03-06",
		Aliases:     []string{"who"},
		GeneratedBy: "test",
		Lang:        "go",
	}
	if err := identity.WriteHolonMD(id, filepath.Join(dir, "HOLON.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestFileName), []byte("schema: holon/v0\nkind: native\nbuild:\n  runner: go-module\n  main: ./cmd/who\nrequires:\n  commands: [go]\n  files: [go.mod]\nartifacts:\n  binary: .op/build/bin/sophia-who\n"), 0644); err != nil {
		t.Fatal(err)
	}
	binaryPath := filepath.Join(dir, ".op", "build", "bin", "sophia-who")
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	resolved, err := ResolveBinary("who")
	if err != nil {
		t.Fatalf("ResolveBinary returned error: %v", err)
	}
	resolvedEval, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		t.Fatalf("EvalSymlinks(resolved) failed: %v", err)
	}
	binaryEval, err := filepath.EvalSymlinks(binaryPath)
	if err != nil {
		t.Fatalf("EvalSymlinks(binaryPath) failed: %v", err)
	}
	if resolvedEval != binaryEval {
		t.Fatalf("resolved = %q, want %q", resolvedEval, binaryEval)
	}
}

func TestExecuteLifecycleBuildAndCleanGoModule(t *testing.T) {
	if _, err := execLookPath("go"); err != nil {
		t.Skip("go command not available")
	}

	root := t.TempDir()
	chdirForHolonTest(t, root)

	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(filepath.Join(dir, "cmd", "demo"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/demo\n\ngo 1.24.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mainSrc := "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"demo\") }\n"
	if err := os.WriteFile(filepath.Join(dir, "cmd", "demo", "main.go"), []byte(mainSrc), 0644); err != nil {
		t.Fatal(err)
	}
	manifest := "schema: holon/v0\nkind: native\nbuild:\n  runner: go-module\nrequires:\n  commands: [go]\n  files: [go.mod]\nartifacts:\n  binary: .op/build/bin/demo\n"
	if err := os.WriteFile(filepath.Join(dir, ManifestFileName), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	buildReport, err := ExecuteLifecycle(OperationBuild, dir)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".op", "build", "bin", "demo")); err != nil {
		t.Fatalf("binary missing after build: %v", err)
	}
	if buildReport.Runner != RunnerGoModule {
		t.Fatalf("runner = %q, want %q", buildReport.Runner, RunnerGoModule)
	}

	cleanReport, err := ExecuteLifecycle(OperationClean, dir)
	if err != nil {
		t.Fatalf("clean failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".op")); !os.IsNotExist(err) {
		t.Fatalf(".op still exists after clean: %v", err)
	}
	if len(cleanReport.Notes) == 0 {
		t.Fatalf("expected clean notes, got %+v", cleanReport)
	}
}

func TestExecuteLifecycleBuildRejectsCrossTargetGoModule(t *testing.T) {
	root := t.TempDir()
	chdirForHolonTest(t, root)

	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(filepath.Join(dir, "cmd", "demo"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/demo\n\ngo 1.24.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd", "demo", "main.go"), []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestFileName), []byte("schema: holon/v0\nkind: native\nbuild:\n  runner: go-module\nrequires:\n  commands: [go]\n  files: [go.mod]\nartifacts:\n  binary: .op/build/bin/demo\n"), 0644); err != nil {
		t.Fatal(err)
	}

	otherTarget := "linux"
	if canonicalRuntimeTarget() == "linux" {
		otherTarget = "windows"
	}

	_, err := ExecuteLifecycle(OperationBuild, dir, BuildOptions{Target: otherTarget, DryRun: true})
	if err == nil {
		t.Fatal("expected cross-target error")
	}
	if !strings.Contains(err.Error(), "cross-target build not implemented") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCMakeRunnerDryRunUsesModeSpecificConfig(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "cmake-demo")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestFileName), []byte("schema: holon/v0\nkind: native\nbuild:\n  runner: cmake\nartifacts:\n  binary: .op/build/bin/demo\n"), 0644); err != nil {
		t.Fatal(err)
	}

	manifest, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}
	var report Report
	ctx := BuildContext{Target: canonicalRuntimeTarget(), Mode: buildModeProfile, DryRun: true}
	if err := (cmakeRunner{}).build(manifest, ctx, &report); err != nil {
		t.Fatalf("cmake dry-run build failed: %v", err)
	}
	if len(report.Commands) != 2 {
		t.Fatalf("commands = %d, want 2", len(report.Commands))
	}
	if !strings.Contains(report.Commands[0], "CMAKE_BUILD_TYPE=RelWithDebInfo") {
		t.Fatalf("configure command missing profile config: %q", report.Commands[0])
	}
	if !strings.Contains(report.Commands[1], "--config RelWithDebInfo") {
		t.Fatalf("build command missing profile config: %q", report.Commands[1])
	}
}

func chdirForHolonTest(t *testing.T, dir string) {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
}

func execLookPath(file string) (string, error) {
	return exec.LookPath(file)
}
