package cli

import (
	"context"
	"testing"

	sophiapb "github.com/organic-programming/sophia-who/gen/go/sophia_who/v1"
)

func TestDialMemHolon_SophiaListIdentities(t *testing.T) {
	root := t.TempDir()
	chdirForTest(t, root)

	seedTransportHolon(t, root, transportHolonSeed{
		dirName:    "sophia-who",
		givenName:  "Sophia",
		familyName: "Who?",
		aliases:    []string{"who", "sophia"},
		lang:       "go",
	})
	seedTransportHolon(t, root, transportHolonSeed{
		dirName:    "atlas",
		binaryName: "atlas",
		givenName:  "atlas",
		familyName: "Holon",
		aliases:    []string{"atlas"},
		lang:       "go",
	})

	ctx := context.Background()

	conn, err := dialMemHolon(ctx, "who")
	if err != nil {
		t.Fatalf("dialMemHolon failed: %v", err)
	}
	defer conn.Close()

	client := sophiapb.NewSophiaWhoServiceClient(conn)
	resp, err := client.ListIdentities(ctx, &sophiapb.ListIdentitiesRequest{
		RootDir: "holons",
	})
	if err != nil {
		t.Fatalf("ListIdentities failed: %v", err)
	}

	if len(resp.Entries) < 2 {
		t.Fatalf("entries = %d, want at least 2", len(resp.Entries))
	}
}
