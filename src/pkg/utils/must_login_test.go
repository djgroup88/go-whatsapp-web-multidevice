package utils

import (
	"testing"

	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

func recoverErr(t *testing.T, fn func()) (err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			var ok bool
			if err, ok = r.(error); !ok {
				t.Fatalf("panicked with a non-error: %#v", r)
			}
		}
	}()
	fn()
	return nil
}

func TestMustLogin(t *testing.T) {
	linkedJID := types.NewJID("6281234567890", types.DefaultUserServer)

	tests := []struct {
		name   string
		client *whatsmeow.Client
		want   error
	}{
		{"nil client", nil, pkgError.ErrWaCLI},
		{"no store", &whatsmeow.Client{}, pkgError.ErrWaNotLoggedIn},
		{"never linked", &whatsmeow.Client{Store: &store.Device{}}, pkgError.ErrWaNotLoggedIn},
		// Linked but offline: this one really is transient, so it must report the
		// retryable message rather than asking for another QR scan.
		{"linked but disconnected", &whatsmeow.Client{Store: &store.Device{ID: &linkedJID}}, pkgError.ErrWaNotConnected},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := recoverErr(t, func() { MustLogin(tt.client) })
			if err != tt.want {
				t.Fatalf("MustLogin() panicked with %v, want %v", err, tt.want)
			}
			// Whichever it is, the caller must see one envelope.
			generic, ok := err.(pkgError.GenericError)
			if !ok {
				t.Fatalf("error %T does not carry an envelope", err)
			}
			if generic.ErrCode() != "INVALID_WA_CLI" {
				t.Fatalf("ErrCode() = %q, want INVALID_WA_CLI", generic.ErrCode())
			}
		})
	}
}
