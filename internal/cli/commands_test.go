package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Organic-Programming/sophia-who/pkg/identity"
)

// seedHolon creates a HOLON.md in a temp subdirectory for testing.
func seedHolon(t *testing.T, root, uuid, givenName string) {
	t.Helper()
	dir := filepath.Join(root, givenName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	id := identity.Identity{
		UUID:        uuid,
		GivenName:   givenName,
		FamilyName:  "Test",
		Motto:       "Testing.",
		Composer:    "Test",
		Clade:       "deterministic/pure",
		Status:      "draft",
		Born:        "2026-01-01",
		GeneratedBy: "test",
		Lang:        "go",
	}
	if err := identity.WriteHolonMD(id, filepath.Join(dir, "HOLON.md")); err != nil {
		t.Fatal(err)
	}
}

func TestVersionCommand(t *testing.T) {
	code := Run([]string{"version"}, "0.1.0-test")
	if code != 0 {
		t.Errorf("version returned %d, want 0", code)
	}
}

func TestHelpCommand(t *testing.T) {
	for _, cmd := range []string{"help", "--help", "-h"} {
		code := Run([]string{cmd}, "0.1.0-test")
		if code != 0 {
			t.Errorf("%s returned %d, want 0", cmd, code)
		}
	}
}

func TestListEmpty(t *testing.T) {
	root := t.TempDir()
	original, _ := os.Getwd()
	defer os.Chdir(original) //nolint:errcheck

	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{"list"}, "0.1.0-test")
	if code != 0 {
		t.Errorf("list (empty) returned %d, want 0", code)
	}
}

func TestListWithHolons(t *testing.T) {
	root := t.TempDir()
	seedHolon(t, root, "list-uuid-1", "Alpha")
	seedHolon(t, root, "list-uuid-2", "Beta")

	original, _ := os.Getwd()
	defer os.Chdir(original) //nolint:errcheck

	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{"list"}, "0.1.0-test")
	if code != 0 {
		t.Errorf("list returned %d, want 0", code)
	}
}

func TestShowMissingUUID(t *testing.T) {
	code := Run([]string{"show"}, "0.1.0-test")
	if code != 1 {
		t.Errorf("show (no uuid) returned %d, want 1", code)
	}
}

func TestShowValidUUID(t *testing.T) {
	root := t.TempDir()
	seedHolon(t, root, "show-uuid-42", "Gamma")

	original, _ := os.Getwd()
	defer os.Chdir(original) //nolint:errcheck

	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{"show", "show-uuid-42"}, "0.1.0-test")
	if code != 0 {
		t.Errorf("show returned %d, want 0", code)
	}
}

func TestShowNotFound(t *testing.T) {
	root := t.TempDir()

	original, _ := os.Getwd()
	defer os.Chdir(original) //nolint:errcheck

	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{"show", "nonexistent"}, "0.1.0-test")
	if code != 1 {
		t.Errorf("show (not found) returned %d, want 1", code)
	}
}

func TestNewWithFlags(t *testing.T) {
	root := t.TempDir()

	original, _ := os.Getwd()
	defer os.Chdir(original) //nolint:errcheck

	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{
		"new",
		"--name", "TestHolon",
		"--family", "CLI",
		"--motto", "Testing OP.",
		"--composer", "Test Suite",
		"--output", filepath.Join(root, "test-holon"),
	}, "0.1.0-test")

	if code != 0 {
		t.Fatalf("new returned %d, want 0", code)
	}

	// Verify the file was created
	holonPath := filepath.Join(root, "test-holon", "HOLON.md")
	if _, err := os.Stat(holonPath); err != nil {
		t.Errorf("HOLON.md not created: %v", err)
	}

	// Verify it's parseable
	data, err := os.ReadFile(holonPath)
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := identity.ParseFrontmatter(data)
	if err != nil {
		t.Fatalf("created HOLON.md not parseable: %v", err)
	}
	if id.GivenName != "TestHolon" {
		t.Errorf("GivenName = %q, want %q", id.GivenName, "TestHolon")
	}
	if id.FamilyName != "CLI" {
		t.Errorf("FamilyName = %q, want %q", id.FamilyName, "CLI")
	}
}

func TestPinMissingUUID(t *testing.T) {
	code := Run([]string{"pin"}, "0.1.0-test")
	if code != 1 {
		t.Errorf("pin (no uuid) returned %d, want 1", code)
	}
}

func TestPinWithFlags(t *testing.T) {
	root := t.TempDir()
	seedHolon(t, root, "pin-uuid-99", "Delta")

	original, _ := os.Getwd()
	defer os.Chdir(original) //nolint:errcheck

	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{
		"pin", "pin-uuid-99",
		"--version", "1.0.0",
		"--tag", "v1.0.0",
		"--commit", "abc123",
	}, "0.1.0-test")

	if code != 0 {
		t.Fatalf("pin returned %d, want 0", code)
	}

	// Verify the pin was written
	path, err := identity.FindByUUID(root, "pin-uuid-99")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := identity.ParseFrontmatter(data)
	if err != nil {
		t.Fatal(err)
	}
	if id.BinaryVersion != "1.0.0" {
		t.Errorf("BinaryVersion = %q, want %q", id.BinaryVersion, "1.0.0")
	}
	if id.GitTag != "v1.0.0" {
		t.Errorf("GitTag = %q, want %q", id.GitTag, "v1.0.0")
	}
}

func TestDiscoverCommand(t *testing.T) {
	root := t.TempDir()
	seedHolon(t, root, "disc-uuid", "Echo")

	original, _ := os.Getwd()
	defer os.Chdir(original) //nolint:errcheck

	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{"discover"}, "0.1.0-test")
	if code != 0 {
		t.Errorf("discover returned %d, want 0", code)
	}
}

func TestDispatchUnknownHolon(t *testing.T) {
	code := Run([]string{"nonexistent-holon", "some-command"}, "0.1.0-test")
	if code != 1 {
		t.Errorf("dispatch (unknown) returned %d, want 1", code)
	}
}

func TestFlagValue(t *testing.T) {
	args := []string{"--name", "Test", "--lang", "rust", "--verbose"}

	if v := flagValue(args, "--name"); v != "Test" {
		t.Errorf("flagValue(--name) = %q, want %q", v, "Test")
	}
	if v := flagValue(args, "--lang"); v != "rust" {
		t.Errorf("flagValue(--lang) = %q, want %q", v, "rust")
	}
	if v := flagValue(args, "--missing"); v != "" {
		t.Errorf("flagValue(--missing) = %q, want empty", v)
	}
	// --verbose has no value after it
	if v := flagValue(args, "--verbose"); v != "" {
		t.Errorf("flagValue(--verbose) = %q, want empty", v)
	}
}

func TestFlagOrDefault(t *testing.T) {
	args := []string{"--name", "Test"}

	if v := flagOrDefault(args, "--name", "fallback"); v != "Test" {
		t.Errorf("flagOrDefault(--name) = %q, want %q", v, "Test")
	}
	if v := flagOrDefault(args, "--missing", "fallback"); v != "fallback" {
		t.Errorf("flagOrDefault(--missing) = %q, want %q", v, "fallback")
	}
}
