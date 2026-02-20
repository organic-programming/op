package cli

import (
	"os"
	"path/filepath"
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

func TestRunWhoListThroughTransportChain(t *testing.T) {
	root := t.TempDir()
	chdirForTest(t, root)

	seedTransportHolon(t, root, "who", "go")
	seedTransportHolon(t, root, "atlas", "go")

	code := Run([]string{"who", "list", "holons"}, "0.1.0-test")
	if code != 0 {
		t.Fatalf("who list returned %d, want 0", code)
	}
}

func TestMapHolonCommandToRPC(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantMethod string
		wantInput  string
		wantErr    bool
	}{
		{
			name:       "list default",
			args:       []string{"list"},
			wantMethod: "ListIdentities",
			wantInput:  "{}",
		},
		{
			name:       "list root dir",
			args:       []string{"list", "holons"},
			wantMethod: "ListIdentities",
			wantInput:  `{"rootDir":"holons"}`,
		},
		{
			name:       "show uuid",
			args:       []string{"show", "abc123"},
			wantMethod: "ShowIdentity",
			wantInput:  `{"uuid":"abc123"}`,
		},
		{
			name:       "pin uuid",
			args:       []string{"pin", "abc123"},
			wantMethod: "PinVersion",
			wantInput:  `{"uuid":"abc123"}`,
		},
		{
			name:       "new with json input",
			args:       []string{"new", `{"givenName":"Alpha"}`},
			wantMethod: "CreateIdentity",
			wantInput:  `{"givenName":"Alpha"}`,
		},
		{
			name:       "custom method passthrough",
			args:       []string{"ListIdentities"},
			wantMethod: "ListIdentities",
			wantInput:  "{}",
		},
		{
			name:    "show missing uuid",
			args:    []string{"show"},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			method, input, err := mapHolonCommandToRPC(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("mapHolonCommandToRPC returned error: %v", err)
			}
			if method != tc.wantMethod {
				t.Fatalf("method = %q, want %q", method, tc.wantMethod)
			}
			if input != tc.wantInput {
				t.Fatalf("input = %q, want %q", input, tc.wantInput)
			}
		})
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

func TestParseGlobalFormat(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantFormat Format
		wantArgs   []string
		wantErr    bool
	}{
		{
			name:       "default format",
			args:       []string{"who", "list"},
			wantFormat: FormatText,
			wantArgs:   []string{"who", "list"},
		},
		{
			name:       "long flag",
			args:       []string{"--format", "json", "who", "list"},
			wantFormat: FormatJSON,
			wantArgs:   []string{"who", "list"},
		},
		{
			name:       "short flag",
			args:       []string{"-f", "json", "who", "list"},
			wantFormat: FormatJSON,
			wantArgs:   []string{"who", "list"},
		},
		{
			name:       "inline long flag",
			args:       []string{"--format=text", "who", "list"},
			wantFormat: FormatText,
			wantArgs:   []string{"who", "list"},
		},
		{
			name:       "inline short flag",
			args:       []string{"-f=text", "who", "list"},
			wantFormat: FormatText,
			wantArgs:   []string{"who", "list"},
		},
		{
			name:       "flag after command is not global",
			args:       []string{"who", "-f", "json", "list"},
			wantFormat: FormatText,
			wantArgs:   []string{"who", "-f", "json", "list"},
		},
		{
			name:    "invalid format",
			args:    []string{"--format", "yaml", "who", "list"},
			wantErr: true,
		},
		{
			name:    "missing format value",
			args:    []string{"-f"},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotFormat, gotArgs, err := parseGlobalFormat(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseGlobalFormat returned error: %v", err)
			}
			if gotFormat != tc.wantFormat {
				t.Fatalf("format = %q, want %q", gotFormat, tc.wantFormat)
			}
			if len(gotArgs) != len(tc.wantArgs) {
				t.Fatalf("args length = %d, want %d", len(gotArgs), len(tc.wantArgs))
			}
			for i := range gotArgs {
				if gotArgs[i] != tc.wantArgs[i] {
					t.Fatalf("args[%d] = %q, want %q", i, gotArgs[i], tc.wantArgs[i])
				}
			}
		})
	}
}
