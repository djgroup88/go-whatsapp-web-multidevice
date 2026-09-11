package error

import (
	"net/http"
	"testing"
)

// TestWaCliErrorContract pins the parts API clients depend on. digisuw's
// GowaClient keys its session-invalid branch on the envelope code and retries on
// any 5xx, so the code must not drift and the status must stay server-class.
func TestWaCliErrorContract(t *testing.T) {
	if got := ErrWaCLI.ErrCode(); got != "INVALID_WA_CLI" {
		t.Fatalf("ErrCode() = %q, callers branch on INVALID_WA_CLI", got)
	}
	if got := ErrWaCLI.StatusCode(); got != http.StatusServiceUnavailable {
		t.Fatalf("StatusCode() = %d, want %d", got, http.StatusServiceUnavailable)
	}
	if got := ErrWaCLI.StatusCode(); got < 500 || got > 599 {
		t.Fatalf("StatusCode() = %d must stay 5xx so retrying clients keep working", got)
	}
}

// TestSessionErrorsShareOneEnvelope guards the unification: every way a device
// can lack a usable session must answer with the same code and status, or a
// client can only recognise some of them. Only the prose may differ.
func TestSessionErrorsShareOneEnvelope(t *testing.T) {
	cases := map[string]WaCliError{
		"no client":     ErrWaCLI,
		"not connected": ErrWaNotConnected,
		"not logged in": ErrWaNotLoggedIn,
	}

	seen := map[string]bool{}
	for name, err := range cases {
		t.Run(name, func(t *testing.T) {
			if got := err.ErrCode(); got != "INVALID_WA_CLI" {
				t.Fatalf("ErrCode() = %q, want INVALID_WA_CLI", got)
			}
			if got := err.StatusCode(); got != http.StatusServiceUnavailable {
				t.Fatalf("StatusCode() = %d, want %d", got, http.StatusServiceUnavailable)
			}
			if err.Error() == "" {
				t.Fatal("message must say which of the three it was")
			}
			if seen[err.Error()] {
				t.Fatalf("message is not distinct: %q", err.Error())
			}
			seen[err.Error()] = true
		})
	}
}

func TestWaReachoutTimelockErrorContract(t *testing.T) {
	if got := ErrWaReachoutTimelock.ErrCode(); got != "WA_REACHOUT_TIMELOCK" {
		t.Fatalf("ErrCode() = %q, want WA_REACHOUT_TIMELOCK", got)
	}
	if got := ErrWaReachoutTimelock.StatusCode(); got != http.StatusTooManyRequests {
		t.Fatalf("StatusCode() = %d, want %d", got, http.StatusTooManyRequests)
	}
}
