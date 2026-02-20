package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/organic-programming/sophia-who/pkg/identity"
)

func TestSelectTransport_OverrideNested(t *testing.T) {
	root := t.TempDir()
	chdirForTest(t, root)

	config := "transport:\n  atlas: tcp://127.0.0.1:9090\n"
	if err := os.WriteFile(".holonconfig", []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	scheme, err := selectTransport("atlas")
	if err != nil {
		t.Fatalf("selectTransport returned error: %v", err)
	}
	if scheme != "tcp" {
		t.Fatalf("scheme = %q, want %q", scheme, "tcp")
	}
}

func TestSelectTransport_OverrideDotKey(t *testing.T) {
	root := t.TempDir()
	chdirForTest(t, root)

	config := "transport.who: stdio://\n"
	if err := os.WriteFile(".holonconfig", []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	scheme, err := selectTransport("who")
	if err != nil {
		t.Fatalf("selectTransport returned error: %v", err)
	}
	if scheme != "stdio" {
		t.Fatalf("scheme = %q, want %q", scheme, "stdio")
	}
}

func TestSelectTransport_GoHolonUsesMem(t *testing.T) {
	root := t.TempDir()
	chdirForTest(t, root)

	seedTransportHolon(t, root, "alpha", "go")

	scheme, err := selectTransport("alpha")
	if err != nil {
		t.Fatalf("selectTransport returned error: %v", err)
	}
	if scheme != "mem" {
		t.Fatalf("scheme = %q, want %q", scheme, "mem")
	}
}

func TestSelectTransport_NonGoFallsBackToStdio(t *testing.T) {
	root := t.TempDir()
	chdirForTest(t, root)

	seedTransportHolon(t, root, "beta", "rust")

	scheme, err := selectTransport("beta")
	if err != nil {
		t.Fatalf("selectTransport returned error: %v", err)
	}
	if scheme != "stdio" {
		t.Fatalf("scheme = %q, want %q", scheme, "stdio")
	}
}

func TestSelectTransport_NotReachable(t *testing.T) {
	root := t.TempDir()
	chdirForTest(t, root)

	_, err := selectTransport("missing")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := err.Error(); got != "holon not reachable" {
		t.Fatalf("error = %q, want %q", got, "holon not reachable")
	}
}

func seedTransportHolon(t *testing.T, root, name, lang string) {
	t.Helper()

	dir := filepath.Join(root, "holons", name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	binaryPath := filepath.Join(dir, name)
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	id := identity.Identity{
		UUID:        "transport-test-" + name,
		GivenName:   name,
		FamilyName:  "Holon",
		Motto:       "Test.",
		Composer:    "test",
		Clade:       "deterministic/pure",
		Status:      "draft",
		Born:        "2026-02-20",
		Aliases:     []string{name},
		GeneratedBy: "test",
		Lang:        lang,
		ProtoStatus: "draft",
	}
	if err := identity.WriteHolonMD(id, filepath.Join(dir, "HOLON.md")); err != nil {
		t.Fatal(err)
	}
}

func chdirForTest(t *testing.T, dir string) {
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
