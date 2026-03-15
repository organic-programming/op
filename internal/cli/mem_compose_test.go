package cli

import "testing"

func TestBuiltInMemTargets(t *testing.T) {
	if !hasMemComposer("grace-op") {
		t.Fatal("expected grace-op mem target to be registered")
	}
	if !hasMemComposer("op") {
		t.Fatal("expected op mem target to be registered")
	}
	if hasMemComposer("gabriel-greet-go") {
		t.Fatal("did not expect external holons to have a built-in mem target")
	}
}
