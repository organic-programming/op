package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/organic-programming/sophia-who/pkg/identity"
)

func TestSelectTransport_SophiaWhoAliasUsesMemWithoutBinary(t *testing.T) {
	root := t.TempDir()
	chdirForTest(t, root)

	seedTransportHolon(t, root, transportHolonSeed{
		dirName:    "sophia-who",
		binaryName: "",
		givenName:  "Sophia",
		familyName: "Who?",
		aliases:    []string{"who", "sophia"},
		lang:       "go",
	})

	scheme, err := selectTransport("who")
	if err != nil {
		t.Fatalf("selectTransport returned error: %v", err)
	}
	if scheme != "mem" {
		t.Fatalf("scheme = %q, want %q", scheme, "mem")
	}
}

func TestSelectTransport_GoHolonWithoutComposerFallsBackToStdio(t *testing.T) {
	root := t.TempDir()
	chdirForTest(t, root)

	seedTransportHolon(t, root, transportHolonSeed{
		dirName:    "beta",
		binaryName: "beta",
		givenName:  "beta",
		familyName: "Holon",
		aliases:    []string{"beta"},
		lang:       "go",
	})

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

type transportHolonSeed struct {
	dirName    string
	binaryName string
	givenName  string
	familyName string
	aliases    []string
	lang       string
}

func seedTransportHolon(t *testing.T, root string, seed transportHolonSeed) {
	t.Helper()

	dir := filepath.Join(root, "holons", seed.dirName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	if seed.binaryName != "" {
		binaryPath := filepath.Join(dir, seed.binaryName)
		if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	id := identity.Identity{
		UUID:        "transport-test-" + seed.dirName,
		GivenName:   seed.givenName,
		FamilyName:  seed.familyName,
		Motto:       "Test.",
		Composer:    "test",
		Clade:       "deterministic/pure",
		Status:      "draft",
		Born:        "2026-02-20",
		Aliases:     seed.aliases,
		GeneratedBy: "test",
		Lang:        seed.lang,
		ProtoStatus: "draft",
	}
	if err := identity.WriteHolonYAML(id, filepath.Join(dir, identity.ManifestFileName)); err != nil {
		t.Fatal(err)
	}
	if seed.binaryName != "" {
		manifest := fmt.Sprintf("%s\nkind: native\nbuild:\n  runner: go-module\nartifacts:\n  binary: %s\n", manifestIdentityPrefix(id), seed.binaryName)
		if err := os.WriteFile(filepath.Join(dir, identity.ManifestFileName), []byte(manifest), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func manifestIdentityPrefix(id identity.Identity) string {
	return fmt.Sprintf(
		"schema: holon/v0\nuuid: %q\ngiven_name: %q\nfamily_name: %q\nmotto: %q\ncomposer: %q\nclade: %q\nstatus: %s\nborn: %q\nparents: []\nreproduction: %q\naliases: [%s]\ngenerated_by: %q\nlang: %q\nproto_status: draft",
		id.UUID,
		id.GivenName,
		id.FamilyName,
		id.Motto,
		id.Composer,
		id.Clade,
		id.Status,
		id.Born,
		id.Reproduction,
		quotedList(id.Aliases),
		id.GeneratedBy,
		id.Lang,
	)
}

func quotedList(values []string) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, fmt.Sprintf("%q", value))
	}
	return strings.Join(out, ", ")
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
