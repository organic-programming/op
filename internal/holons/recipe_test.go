package holons

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeRecipeManifest(t *testing.T, dir, yaml string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ManifestFileName), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadManifestAcceptsCompositeRecipe(t *testing.T) {
	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "child-a"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "child-a", ManifestFileName), []byte(`schema: holon/v0
kind: native
build:
  runner: go-module
requires:
  commands: [go]
  files: [go.mod]
artifacts:
  binary: child-a
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "child-b"), 0755); err != nil {
		t.Fatal(err)
	}

	writeRecipeManifest(t, root, `schema: holon/v0
kind: composite
build:
  runner: recipe
  defaults:
    target: macos
    mode: debug
  members:
    - id: a
      path: child-a
      type: holon
    - id: b
      path: child-b
      type: component
  targets:
    macos:
      steps:
        - build_member: a
        - exec:
            cwd: child-b
            argv: ["echo", "hello"]
artifacts:
  primary: child-b/my-app
`)

	loaded, err := LoadManifest(root)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}
	if loaded.Manifest.Kind != KindComposite {
		t.Fatalf("kind = %q, want %q", loaded.Manifest.Kind, KindComposite)
	}
	if loaded.Manifest.Build.Runner != RunnerRecipe {
		t.Fatalf("runner = %q, want %q", loaded.Manifest.Build.Runner, RunnerRecipe)
	}
	if got := loaded.Manifest.Build.Defaults.Target; got != "macos" {
		t.Fatalf("defaults.target = %q, want %q", got, "macos")
	}
}

func TestLoadManifestNormalizesDarwinRecipeTargets(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "work"), 0755); err != nil {
		t.Fatal(err)
	}

	writeRecipeManifest(t, root, `schema: holon/v0
kind: composite
build:
  runner: recipe
  defaults:
    target: darwin
    mode: DEBUG
  members:
    - id: work
      path: work
      type: component
  targets:
    darwin:
      steps:
        - exec:
            cwd: work
            argv: ["echo", "hello"]
artifacts:
  primary_by_target:
    darwin:
      debug: work/app-debug
`)

	loaded, err := LoadManifest(root)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}
	if got := loaded.Manifest.Build.Defaults.Target; got != "macos" {
		t.Fatalf("defaults.target = %q, want %q", got, "macos")
	}
	if got := loaded.Manifest.Build.Defaults.Mode; got != "debug" {
		t.Fatalf("defaults.mode = %q, want %q", got, "debug")
	}
	if _, ok := loaded.Manifest.Build.Targets["macos"]; !ok {
		t.Fatalf("expected normalized macos target, got %v", loaded.Manifest.Build.Targets)
	}
	if _, ok := loaded.Manifest.Artifacts.PrimaryByTarget["macos"]; !ok {
		t.Fatalf("expected normalized macos artifact entry, got %v", loaded.Manifest.Artifacts.PrimaryByTarget)
	}
}

func TestRecipeValidationRejectsDuplicateNormalizedTargets(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "work"), 0755); err != nil {
		t.Fatal(err)
	}

	writeRecipeManifest(t, root, `schema: holon/v0
kind: composite
build:
  runner: recipe
  members:
    - id: work
      path: work
      type: component
  targets:
    macos:
      steps:
        - exec:
            cwd: work
            argv: ["echo", "macos"]
    darwin:
      steps:
        - exec:
            cwd: work
            argv: ["echo", "darwin"]
artifacts:
  primary: work/my-app
`)

	_, err := LoadManifest(root)
	if err == nil {
		t.Fatal("expected duplicate normalized target error")
	}
	if !strings.Contains(err.Error(), "duplicate recipe target") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRecipeValidationRejectsUnknownMemberRef(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "child"), 0755); err != nil {
		t.Fatal(err)
	}

	writeRecipeManifest(t, root, `schema: holon/v0
kind: composite
build:
  runner: recipe
  members:
    - id: child
      path: child
      type: component
  targets:
    macos:
      steps:
        - build_member: nonexistent
artifacts:
  primary: child/my-app
`)

	_, err := LoadManifest(root)
	if err == nil {
		t.Fatal("expected error for unknown member ref")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRecipeValidationRejectsMultiActionStep(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "child"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "child", ManifestFileName), []byte(`schema: holon/v0
kind: native
build:
  runner: go-module
requires:
  commands: [go]
  files: [go.mod]
artifacts:
  binary: child
`), 0644); err != nil {
		t.Fatal(err)
	}

	writeRecipeManifest(t, root, `schema: holon/v0
kind: composite
build:
  runner: recipe
  members:
    - id: child
      path: child
      type: holon
  targets:
    macos:
      steps:
        - build_member: child
          exec:
            cwd: child
            argv: ["echo", "oops"]
artifacts:
  primary: child/my-app
`)

	_, err := LoadManifest(root)
	if err == nil {
		t.Fatal("expected multi-action step error")
	}
	if !strings.Contains(err.Error(), "exactly one action") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRecipeBuildAllDryRunBuildsEachDeclaredTarget(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "app"), 0755); err != nil {
		t.Fatal(err)
	}

	writeRecipeManifest(t, root, `schema: holon/v0
kind: composite
platforms: [macos, ios-simulator]
build:
  runner: recipe
  defaults:
    mode: debug
  members:
    - id: app
      path: app
      type: component
  targets:
    macos:
      steps:
        - exec:
            cwd: app
            argv: ["echo", "macos"]
    ios-simulator:
      steps:
        - exec:
            cwd: app
            argv: ["echo", "ios-simulator"]
artifacts:
  primary_by_target:
    macos:
      debug: app/macos.app
    ios-simulator:
      debug: app/ios-simulator.app
`)

	report, err := ExecuteLifecycle(OperationBuild, root, BuildOptions{
		Target: "all",
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("ExecuteLifecycle(build all) failed: %v", err)
	}

	if report.BuildTarget != "all" {
		t.Fatalf("report.BuildTarget = %q, want %q", report.BuildTarget, "all")
	}
	if report.Artifact != "" {
		t.Fatalf("report.Artifact = %q, want empty for aggregate builds", report.Artifact)
	}
	if len(report.Children) != 2 {
		t.Fatalf("len(report.Children) = %d, want 2", len(report.Children))
	}

	gotTargets := []string{report.Children[0].BuildTarget, report.Children[1].BuildTarget}
	wantTargets := []string{"macos", "ios-simulator"}
	if !slices.Equal(gotTargets, wantTargets) {
		t.Fatalf("child targets = %v, want %v", gotTargets, wantTargets)
	}
}

func TestRecipeValidationRejectsBuildMemberForComponent(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "component"), 0755); err != nil {
		t.Fatal(err)
	}

	writeRecipeManifest(t, root, `schema: holon/v0
kind: composite
build:
  runner: recipe
  members:
    - id: component
      path: component
      type: component
  targets:
    macos:
      steps:
        - build_member: component
artifacts:
  primary: component/my-app
`)

	_, err := LoadManifest(root)
	if err == nil {
		t.Fatal("expected component build_member error")
	}
	if !strings.Contains(err.Error(), "must reference a holon member") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRecipeValidationRejectsExecWithoutArgv(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "work"), 0755); err != nil {
		t.Fatal(err)
	}

	writeRecipeManifest(t, root, `schema: holon/v0
kind: composite
build:
  runner: recipe
  members:
    - id: work
      path: work
      type: component
  targets:
    macos:
      steps:
        - exec:
            cwd: work
            argv: []
artifacts:
  primary: work/my-app
`)

	_, err := LoadManifest(root)
	if err == nil {
		t.Fatal("expected empty argv error")
	}
	if !strings.Contains(err.Error(), "exec.argv") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRecipeValidationRejectsEscapingPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "work"), 0755); err != nil {
		t.Fatal(err)
	}

	writeRecipeManifest(t, root, `schema: holon/v0
kind: composite
build:
  runner: recipe
  members:
    - id: work
      path: ../escape
      type: component
  targets:
    macos:
      steps:
        - exec:
            cwd: work
            argv: ["echo", "hello"]
artifacts:
  primary: work/my-app
`)

	_, err := LoadManifest(root)
	if err == nil {
		t.Fatal("expected escaping path error")
	}
	if !strings.Contains(err.Error(), "within the manifest directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRecipeRunnerExecStep(t *testing.T) {
	if runtimePlatform() == "windows" {
		t.Skip("touch not available on Windows test environment")
	}

	root := t.TempDir()
	chdirForHolonTest(t, root)
	workDir := filepath.Join(root, "work")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatal(err)
	}

	writeRecipeManifest(t, root, `schema: holon/v0
kind: composite
build:
  runner: recipe
  members:
    - id: work
      path: work
      type: component
  targets:
    macos:
      steps:
        - exec:
            cwd: work
            argv: ["touch", "proof.txt"]
    linux:
      steps:
        - exec:
            cwd: work
            argv: ["touch", "proof.txt"]
artifacts:
  primary: work/proof.txt
`)

	report, err := ExecuteLifecycle(OperationBuild, root)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "proof.txt")); err != nil {
		t.Fatalf("exec step did not create proof file: %v", err)
	}
	if len(report.Commands) == 0 || !strings.Contains(report.Commands[0], "touch") {
		t.Fatalf("expected touch in commands, got %v", report.Commands)
	}
}

func TestRecipeRunnerBuildTargetOverride(t *testing.T) {
	if runtimePlatform() == "windows" {
		t.Skip("touch not available on Windows test environment")
	}

	root := t.TempDir()
	chdirForHolonTest(t, root)
	if err := os.MkdirAll(filepath.Join(root, "work"), 0755); err != nil {
		t.Fatal(err)
	}

	writeRecipeManifest(t, root, `schema: holon/v0
kind: composite
build:
  runner: recipe
  defaults:
    target: macos
  members:
    - id: work
      path: work
      type: component
  targets:
    macos:
      steps:
        - exec:
            cwd: work
            argv: ["touch", "macos.txt"]
    linux:
      steps:
        - exec:
            cwd: work
            argv: ["touch", "linux.txt"]
artifacts:
  primary_by_target:
    macos:
      debug: work/macos.txt
    linux:
      debug: work/linux.txt
`)

	report, err := ExecuteLifecycle(OperationBuild, root, BuildOptions{Target: "linux"})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "work", "linux.txt")); err != nil {
		t.Fatalf("linux target file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "work", "macos.txt")); err == nil {
		t.Fatal("macos target file should not have been created")
	}
	if report.BuildTarget != "linux" {
		t.Fatalf("build target = %q, want linux", report.BuildTarget)
	}
	if !strings.HasSuffix(report.Artifact, "work/linux.txt") {
		t.Fatalf("artifact = %q, want linux artifact", report.Artifact)
	}
}

func TestRecipeRunnerCopyStep(t *testing.T) {
	root := t.TempDir()
	chdirForHolonTest(t, root)
	srcDir := filepath.Join(root, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "data.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	writeRecipeManifest(t, root, `schema: holon/v0
kind: composite
build:
  runner: recipe
  members:
    - id: src
      path: src
      type: component
  targets:
    macos:
      steps:
        - copy:
            from: src/data.txt
            to: dst/data.txt
    linux:
      steps:
        - copy:
            from: src/data.txt
            to: dst/data.txt
artifacts:
  primary: dst/data.txt
`)

	report, err := ExecuteLifecycle(OperationBuild, root)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "dst", "data.txt"))
	if err != nil {
		t.Fatalf("copy step did not produce file: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("copied content = %q, want hello", string(data))
	}
	if len(report.Commands) == 0 || !strings.Contains(report.Commands[0], "copy") {
		t.Fatalf("expected copy command, got %v", report.Commands)
	}
}

func TestRecipeRunnerAssertFilePass(t *testing.T) {
	root := t.TempDir()
	chdirForHolonTest(t, root)
	if err := os.MkdirAll(filepath.Join(root, "out"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "out", "result.bin"), []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}

	writeRecipeManifest(t, root, `schema: holon/v0
kind: composite
build:
  runner: recipe
  members:
    - id: out
      path: out
      type: component
  targets:
    macos:
      steps:
        - assert_file:
            path: out/result.bin
    linux:
      steps:
        - assert_file:
            path: out/result.bin
artifacts:
  primary: out/result.bin
`)

	if _, err := ExecuteLifecycle(OperationBuild, root); err != nil {
		t.Fatalf("assert_file pass case failed: %v", err)
	}
}

func TestRecipeRunnerAssertFileFail(t *testing.T) {
	root := t.TempDir()
	chdirForHolonTest(t, root)
	if err := os.MkdirAll(filepath.Join(root, "out"), 0755); err != nil {
		t.Fatal(err)
	}

	writeRecipeManifest(t, root, `schema: holon/v0
kind: composite
build:
  runner: recipe
  members:
    - id: out
      path: out
      type: component
  targets:
    macos:
      steps:
        - assert_file:
            path: out/missing.bin
    linux:
      steps:
        - assert_file:
            path: out/missing.bin
artifacts:
  primary: out/result.bin
`)

	_, err := ExecuteLifecycle(OperationBuild, root)
	if err == nil {
		t.Fatal("expected error for missing assert_file")
	}
	if !strings.Contains(err.Error(), "assert_file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRecipeRunnerMissingTarget(t *testing.T) {
	root := t.TempDir()
	chdirForHolonTest(t, root)
	if err := os.MkdirAll(filepath.Join(root, "child"), 0755); err != nil {
		t.Fatal(err)
	}

	writeRecipeManifest(t, root, `schema: holon/v0
kind: composite
build:
  runner: recipe
  members:
    - id: child
      path: child
      type: component
  targets:
    windows:
      steps:
        - exec:
            cwd: child
            argv: ["echo", "hello"]
artifacts:
  primary: child/my-app
`)

	_, err := ExecuteLifecycle(OperationBuild, root)
	if err == nil {
		t.Fatal("expected error for missing target")
	}
	if !strings.Contains(err.Error(), "no recipe target") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDryRunReportsPlanAndArtifact(t *testing.T) {
	root := t.TempDir()
	chdirForHolonTest(t, root)
	if err := os.MkdirAll(filepath.Join(root, "work"), 0755); err != nil {
		t.Fatal(err)
	}

	writeRecipeManifest(t, root, `schema: holon/v0
kind: composite
build:
  runner: recipe
  members:
    - id: work
      path: work
      type: component
  targets:
    linux:
      steps:
        - exec:
            cwd: work
            argv: ["touch", "linux-release.txt"]
artifacts:
  primary_by_target:
    linux:
      release: work/linux-release.txt
`)

	report, err := ExecuteLifecycle(OperationBuild, root, BuildOptions{
		Target: "linux",
		Mode:   "release",
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("dry run failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "work", "linux-release.txt")); err == nil {
		t.Fatal("dry run should not have created the file")
	}
	if len(report.Commands) == 0 || !strings.Contains(report.Commands[0], "touch") {
		t.Fatalf("expected planned command, got %v", report.Commands)
	}
	if report.Artifact == "" || !strings.HasSuffix(report.Artifact, "work/linux-release.txt") {
		t.Fatalf("artifact = %q, want linux release artifact", report.Artifact)
	}
	foundDryRun := false
	for _, note := range report.Notes {
		if strings.Contains(note, "dry run") {
			foundDryRun = true
		}
	}
	if !foundDryRun {
		t.Fatalf("expected dry run note, got %v", report.Notes)
	}
}

func TestRecipeRunnerPropagatesBuildContextToChildren(t *testing.T) {
	root := t.TempDir()
	chdirForHolonTest(t, root)
	childDir := filepath.Join(root, "child")
	if err := os.MkdirAll(filepath.Join(childDir, "work"), 0755); err != nil {
		t.Fatal(err)
	}

	writeRecipeManifest(t, childDir, `schema: holon/v0
kind: composite
build:
  runner: recipe
  members:
    - id: work
      path: work
      type: component
  targets:
    macos:
      steps:
        - exec:
            cwd: work
            argv: ["touch", "macos-release.txt"]
    linux:
      steps:
        - exec:
            cwd: work
            argv: ["touch", "linux-release.txt"]
artifacts:
  primary_by_target:
    macos:
      release: work/macos-release.txt
    linux:
      release: work/linux-release.txt
`)

	writeRecipeManifest(t, root, `schema: holon/v0
kind: composite
build:
  runner: recipe
  members:
    - id: child
      path: child
      type: holon
  targets:
    linux:
      steps:
        - build_member: child
artifacts:
  primary_by_target:
    linux:
      release: child/work/linux-release.txt
`)

	report, err := ExecuteLifecycle(OperationBuild, root, BuildOptions{
		Target: "linux",
		Mode:   "release",
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("dry run failed: %v", err)
	}
	if len(report.Children) != 1 {
		t.Fatalf("children = %d, want 1", len(report.Children))
	}
	child := report.Children[0]
	if child.BuildTarget != "linux" {
		t.Fatalf("child build target = %q, want linux", child.BuildTarget)
	}
	if child.BuildMode != "release" {
		t.Fatalf("child build mode = %q, want release", child.BuildMode)
	}
	if !strings.HasSuffix(child.Artifact, "child/work/linux-release.txt") {
		t.Fatalf("child artifact = %q, want linux release artifact", child.Artifact)
	}
	foundLinuxCommand := false
	for _, command := range child.Commands {
		if strings.Contains(command, "linux-release.txt") {
			foundLinuxCommand = true
		}
	}
	if !foundLinuxCommand {
		t.Fatalf("expected child commands to reflect propagated target/mode, got %v", child.Commands)
	}
}
