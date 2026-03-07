package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/organic-programming/grace-op/internal/holons"
)

// selectTransport determines the best transport for a target holon.
// Priority:
//  1. Already running (known endpoint) -> dial existing
//  2. Supported in-process holon -> mem:// (lazy in-process)
//  3. Binary available locally -> stdio:// (ephemeral)
//  4. Network reachable -> tcp://
func selectTransport(holonName string) (scheme string, err error) {
	target, targetErr := holons.ResolveTarget(holonName)
	if targetErr == nil && supportsMemTransport(holonName, target) {
		return "mem", nil
	}

	binaryPath, err := resolveHolon(holonName)
	if binaryPath != "" {
		return "stdio", nil
	}

	return "", fmt.Errorf("holon not reachable")
}

func supportsMemTransport(requested string, target *holons.Target) bool {
	if target == nil {
		return false
	}
	if !isGoTransportTarget(target) {
		return false
	}

	for _, candidate := range memCandidateNames(requested, target) {
		if hasMemComposer(candidate) {
			return true
		}
	}
	return false
}

func isGoTransportTarget(target *holons.Target) bool {
	if target == nil {
		return false
	}
	if target.Identity != nil && strings.EqualFold(strings.TrimSpace(target.Identity.Lang), "go") {
		return true
	}
	return target.Manifest != nil && strings.EqualFold(strings.TrimSpace(target.Manifest.Manifest.Build.Runner), holons.RunnerGoModule)
}

func memCandidateNames(requested string, target *holons.Target) []string {
	names := []string{requested}
	if target == nil {
		return names
	}
	names = append(names, filepath.Base(target.Dir))
	return names
}
