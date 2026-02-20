package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/organic-programming/sophia-who/pkg/identity"
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

func TestPromotedVerbsDelegateToWho(t *testing.T) {
	type call struct {
		holon string
		args  []string
	}
	var calls []call

	originalDispatch := dispatchPromoted
	dispatchPromoted = func(holon string, args []string) int {
		calls = append(calls, call{holon: holon, args: append([]string(nil), args...)})
		return 0
	}
	defer func() { dispatchPromoted = originalDispatch }()

	tests := []struct {
		name      string
		input     []string
		wantHolon string
		wantArgs  []string
	}{
		{
			name:      "new",
			input:     []string{"new", "--name", "Alpha"},
			wantHolon: "who",
			wantArgs:  []string{"new", "--name", "Alpha"},
		},
		{
			name:      "list",
			input:     []string{"list", "/tmp/project"},
			wantHolon: "who",
			wantArgs:  []string{"list", "/tmp/project"},
		},
		{
			name:      "show",
			input:     []string{"show", "abcd"},
			wantHolon: "who",
			wantArgs:  []string{"show", "abcd"},
		},
		{
			name:      "pin",
			input:     []string{"pin", "abcd", "--version", "1.0.0"},
			wantHolon: "who",
			wantArgs:  []string{"pin", "abcd", "--version", "1.0.0"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code := Run(tc.input, "0.1.0-test")
			if code != 0 {
				t.Fatalf("Run returned %d, want 0", code)
			}
		})
	}

	if len(calls) != len(tests) {
		t.Fatalf("dispatch called %d times, want %d", len(calls), len(tests))
	}

	for i, tc := range tests {
		got := calls[i]
		if got.holon != tc.wantHolon {
			t.Fatalf("call %d holon = %q, want %q", i, got.holon, tc.wantHolon)
		}
		if !reflect.DeepEqual(got.args, tc.wantArgs) {
			t.Fatalf("call %d args = %#v, want %#v", i, got.args, tc.wantArgs)
		}
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
