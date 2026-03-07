package holons

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	openv "github.com/organic-programming/grace-op/internal/env"
)

type InstallOptions struct {
	NoBuild bool
}

type InstallReport struct {
	Operation   string   `json:"operation"`
	Target      string   `json:"target"`
	Holon       string   `json:"holon"`
	Dir         string   `json:"dir,omitempty"`
	Manifest    string   `json:"manifest,omitempty"`
	Binary      string   `json:"binary,omitempty"`
	BuildTarget string   `json:"build_target,omitempty"`
	BuildMode   string   `json:"build_mode,omitempty"`
	Artifact    string   `json:"artifact,omitempty"`
	Installed   string   `json:"installed,omitempty"`
	Notes       []string `json:"notes,omitempty"`
}

func Install(ref string, opts InstallOptions) (InstallReport, error) {
	target, err := ResolveTarget(ref)
	if err != nil {
		return InstallReport{
			Operation: "install",
			Target:    normalizedTarget(ref),
		}, err
	}
	if target.ManifestErr != nil {
		return baseInstallReport("install", target, BuildContext{}), target.ManifestErr
	}
	if target.Manifest == nil {
		return baseInstallReport("install", target, BuildContext{}), fmt.Errorf("no %s found in %s", ManifestFileName, target.RelativePath)
	}

	ctx, err := resolveBuildContext(target.Manifest, BuildOptions{})
	if err != nil {
		return baseInstallReport("install", target, BuildContext{}), err
	}

	report := baseInstallReport("install", target, ctx)
	binaryName := target.Manifest.BinaryName()
	if binaryName == "" {
		return report, fmt.Errorf("holon %q has no installable binary (composite with artifacts.primary only)", report.Holon)
	}
	report.Binary = binaryName

	artifactPath := target.Manifest.BinaryPath()
	report.Artifact = workspaceRelativePath(artifactPath)

	primaryArtifactPath := target.Manifest.PrimaryArtifactPath(ctx)
	if primaryArtifactPath != "" && filepath.Clean(primaryArtifactPath) != filepath.Clean(artifactPath) {
		return report, fmt.Errorf("holon %q has non-binary primary artifact %s; non-binary install is out of scope", report.Holon, workspaceRelativePath(primaryArtifactPath))
	}

	if _, err := os.Stat(artifactPath); err != nil {
		if !os.IsNotExist(err) {
			return report, err
		}
		if opts.NoBuild {
			return report, fmt.Errorf("artifact missing: %s", report.Artifact)
		}

		_, buildErr := ExecuteLifecycle(OperationBuild, ref)
		if buildErr != nil {
			report.Notes = append(report.Notes, "build failed before install")
			return report, buildErr
		}
		report.Notes = append(report.Notes, "artifact missing; built before install")
		if _, statErr := os.Stat(artifactPath); statErr != nil {
			if os.IsNotExist(statErr) {
				return report, fmt.Errorf("artifact missing: %s", report.Artifact)
			}
			return report, statErr
		}
	}

	if err := openv.Init(); err != nil {
		return report, fmt.Errorf("prepare %s: %w", openv.OPBIN(), err)
	}

	installedPath := filepath.Join(openv.OPBIN(), binaryName)
	if err := copyFile(artifactPath, installedPath); err != nil {
		return report, fmt.Errorf("install %s: %w", binaryName, err)
	}
	report.Installed = installedPath
	report.Notes = append(report.Notes, "installed into "+installedPath)
	return report, nil
}

func Uninstall(ref string) (InstallReport, error) {
	target, err := ResolveTarget(ref)
	if err == nil && target.ManifestErr != nil {
		return baseInstallReport("uninstall", target, BuildContext{}), target.ManifestErr
	}

	report := InstallReport{
		Operation: "uninstall",
		Target:    normalizedTarget(ref),
	}
	if err == nil {
		report = baseInstallReport("uninstall", target, BuildContext{})
	}

	binaryName := strings.TrimSpace(ref)
	if err == nil {
		if manifestBinary := target.Manifest.BinaryName(); manifestBinary != "" {
			binaryName = manifestBinary
		} else {
			binaryName = binaryNameForTarget(target, "")
		}
	}
	if binaryName == "" {
		return report, fmt.Errorf("cannot resolve install name for %q", ref)
	}
	report.Binary = binaryName

	installedPath := filepath.Join(openv.OPBIN(), binaryName)
	report.Installed = installedPath
	if removeErr := os.Remove(installedPath); removeErr != nil {
		if os.IsNotExist(removeErr) {
			report.Notes = append(report.Notes, "not installed")
			return report, nil
		}
		return report, fmt.Errorf("remove %s: %w", installedPath, removeErr)
	}
	report.Notes = append(report.Notes, "removed "+installedPath)
	return report, nil
}

func baseInstallReport(operation string, target *Target, ctx BuildContext) InstallReport {
	report := InstallReport{
		Operation:   operation,
		Target:      normalizedTarget(target.Ref),
		Holon:       filepath.Base(target.Dir),
		Dir:         target.RelativePath,
		BuildTarget: ctx.Target,
		BuildMode:   ctx.Mode,
	}
	if ref := strings.TrimSpace(target.Ref); ref != "" && ref != "." && !strings.ContainsAny(ref, `/\`) {
		report.Holon = ref
	}
	if target.Manifest != nil {
		report.Manifest = workspaceRelativePath(target.Manifest.Path)
	}
	return report
}

func binaryNameForTarget(target *Target, artifactPath string) string {
	if target != nil && target.Manifest != nil {
		if binary := target.Manifest.BinaryName(); binary != "" {
			return binary
		}
	}
	if trimmed := strings.TrimSpace(artifactPath); trimmed != "" {
		return filepath.Base(trimmed)
	}
	if target != nil {
		return filepath.Base(target.Dir)
	}
	return ""
}

func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", src)
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}

	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := os.Chmod(tmp, info.Mode()); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
