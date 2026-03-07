package holons

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/organic-programming/sophia-who/pkg/identity"
)

func TestDiscoverHolonsRecursesAndSkipsExcludedDirs(t *testing.T) {
	root := t.TempDir()

	writeDiscoveryHolon(t, filepath.Join(root, "holons", "alpha"), discoveryHolonSeed{
		uuid:       "alpha-uuid",
		givenName:  "Alpha",
		familyName: "Go",
		binaryName: "alpha",
	})
	writeDiscoveryHolon(t, filepath.Join(root, "recipes", "beta"), discoveryHolonSeed{
		uuid:       "beta-uuid",
		givenName:  "Beta",
		familyName: "Rust",
		binaryName: "beta",
	})
	for _, skipped := range []string{
		filepath.Join(root, ".git", "ignored"),
		filepath.Join(root, ".op", "ignored"),
		filepath.Join(root, "node_modules", "ignored"),
		filepath.Join(root, "vendor", "ignored"),
		filepath.Join(root, "build", "ignored"),
		filepath.Join(root, ".hidden", "ignored"),
	} {
		writeDiscoveryHolon(t, skipped, discoveryHolonSeed{
			uuid:       filepath.Base(skipped) + "-uuid",
			givenName:  "Ignored",
			familyName: "Holon",
			binaryName: "ignored-holon",
		})
	}

	located, err := DiscoverHolons(root)
	if err != nil {
		t.Fatalf("DiscoverHolons returned error: %v", err)
	}
	if len(located) != 2 {
		t.Fatalf("located = %d, want 2", len(located))
	}

	got := make(map[string]string, len(located))
	for _, holon := range located {
		got[holon.Identity.UUID] = filepath.ToSlash(holon.RelativePath)
	}
	if got["alpha-uuid"] != "holons/alpha" {
		t.Fatalf("alpha relative path = %q, want %q", got["alpha-uuid"], "holons/alpha")
	}
	if got["beta-uuid"] != "recipes/beta" {
		t.Fatalf("beta relative path = %q, want %q", got["beta-uuid"], "recipes/beta")
	}
}

func TestDiscoverHolonsDedupsSameUUIDClosestToRoot(t *testing.T) {
	root := t.TempDir()

	writeDiscoveryHolon(t, filepath.Join(root, "rob-go"), discoveryHolonSeed{
		uuid:       "same-uuid",
		givenName:  "Rob",
		familyName: "Go",
		binaryName: "rob-go",
	})
	writeDiscoveryHolon(t, filepath.Join(root, "nested", "rob-go"), discoveryHolonSeed{
		uuid:       "same-uuid",
		givenName:  "Rob",
		familyName: "Go",
		binaryName: "rob-go",
	})

	located, err := DiscoverHolons(root)
	if err != nil {
		t.Fatalf("DiscoverHolons returned error: %v", err)
	}
	if len(located) != 1 {
		t.Fatalf("located = %d, want 1", len(located))
	}
	if got := filepath.Base(located[0].Dir); got != "rob-go" {
		t.Fatalf("dir basename = %q, want %q", got, "rob-go")
	}
	if got := filepath.ToSlash(located[0].RelativePath); got != "rob-go" {
		t.Fatalf("relative path = %q, want %q", got, "rob-go")
	}
}

func TestResolveTargetRejectsAmbiguousSlugWithDifferentUUIDs(t *testing.T) {
	root := t.TempDir()
	chdirForHolonTest(t, root)

	writeDiscoveryHolon(t, filepath.Join(root, "team-a", "rob-go"), discoveryHolonSeed{
		uuid:       "c7f3a1b2-1111-1111-1111-111111111111",
		givenName:  "Rob",
		familyName: "Go",
		binaryName: "rob-go",
	})
	writeDiscoveryHolon(t, filepath.Join(root, "team-b", "rob-go"), discoveryHolonSeed{
		uuid:       "d8e0f1a2-2222-2222-2222-222222222222",
		givenName:  "Rob",
		familyName: "Go",
		binaryName: "rob-go",
	})

	_, err := ResolveTarget("rob-go")
	if err == nil {
		t.Fatal("expected ambiguous slug error")
	}
	if !strings.Contains(err.Error(), `ambiguous holon "rob-go"`) {
		t.Fatalf("error = %q, want ambiguous holon", err.Error())
	}
	if !strings.Contains(err.Error(), "./team-a/rob-go") || !strings.Contains(err.Error(), "./team-b/rob-go") {
		t.Fatalf("error missing disambiguation paths: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "c7f3a1b2") || !strings.Contains(err.Error(), "d8e0f1a2") {
		t.Fatalf("error missing UUID prefixes: %q", err.Error())
	}
}

func TestResolveTargetUsesShallowestMatchForSameSlugAndUUID(t *testing.T) {
	root := t.TempDir()
	chdirForHolonTest(t, root)

	writeDiscoveryHolon(t, filepath.Join(root, "rob-go"), discoveryHolonSeed{
		uuid:       "same-uuid",
		givenName:  "Rob",
		familyName: "Go",
		binaryName: "rob-go",
	})
	writeDiscoveryHolon(t, filepath.Join(root, "nested", "rob-go"), discoveryHolonSeed{
		uuid:       "same-uuid",
		givenName:  "Rob",
		familyName: "Go",
		binaryName: "rob-go",
	})

	target, err := ResolveTarget("rob-go")
	if err != nil {
		t.Fatalf("ResolveTarget returned error: %v", err)
	}
	gotDir, err := filepath.EvalSymlinks(target.Dir)
	if err != nil {
		gotDir = filepath.Clean(target.Dir)
	}
	wantDir, err := filepath.EvalSymlinks(filepath.Join(root, "rob-go"))
	if err != nil {
		wantDir = filepath.Join(root, "rob-go")
	}
	if gotDir != wantDir {
		t.Fatalf("target dir = %q, want %q", gotDir, wantDir)
	}
}

func TestResolveTargetDoesNotUseAliasesOrGivenNames(t *testing.T) {
	root := t.TempDir()
	chdirForHolonTest(t, root)

	dir := filepath.Join(root, "sophia-who")
	if err := os.MkdirAll(dir, 0o755); err != nil {
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
	writeManifestWithIdentity(t, dir, id, "kind: native\nbuild:\n  runner: go-module\nartifacts:\n  binary: sophia-who\n")

	if _, err := ResolveTarget("who"); err == nil {
		t.Fatal("expected alias lookup to fail")
	}
	if _, err := ResolveTarget("Sophia"); err == nil {
		t.Fatal("expected given-name lookup to fail")
	}
}

type discoveryHolonSeed struct {
	uuid       string
	givenName  string
	familyName string
	binaryName string
}

func writeDiscoveryHolon(t *testing.T, dir string, seed discoveryHolonSeed) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	id := identity.Identity{
		UUID:        seed.uuid,
		GivenName:   seed.givenName,
		FamilyName:  seed.familyName,
		Motto:       "Test.",
		Composer:    "test",
		Clade:       "deterministic/pure",
		Status:      "draft",
		Born:        "2026-03-07",
		GeneratedBy: "test",
		Lang:        "go",
	}
	writeManifestWithIdentity(t, dir, id, "kind: native\nbuild:\n  runner: go-module\nartifacts:\n  binary: "+seed.binaryName+"\n")
}
