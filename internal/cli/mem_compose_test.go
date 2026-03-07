package cli

import (
	"context"
	"testing"
)

func TestDialMemHolonFailsWithoutRegisteredComposer(t *testing.T) {
	_, err := dialMemHolon(context.Background(), "who")
	if err == nil {
		t.Fatal("expected dialMemHolon to fail")
	}
}
