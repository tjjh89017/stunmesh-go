package wg

import (
	"errors"
	"testing"
)

func TestElevationHint_NilPassthrough(t *testing.T) {
	if got := elevationHint(nil); got != nil {
		t.Fatalf("elevationHint(nil) = %v, want nil", got)
	}
}

func TestElevationHint_OrdinaryErrorPassthrough(t *testing.T) {
	// A plain error is not access-denied on any platform, so it must pass
	// through unwrapped.
	err := errors.New("device not found")
	if got := elevationHint(err); got != err {
		t.Fatalf("elevationHint(%v) = %v, want identity", err, got)
	}
	if errors.Is(elevationHint(err), ErrElevationRequired) {
		t.Fatal("ordinary error must not be marked ErrElevationRequired")
	}
}
