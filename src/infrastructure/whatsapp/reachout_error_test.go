package whatsapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// reachoutErr mimics the error whatsmeow produces for server error 463:
// fmt.Errorf("%w %d", ErrServerReturnedError, 463).
func reachoutErr() error {
	return errors.Join(whatsmeow.ErrServerReturnedError, errors.New("server returned error 463"))
}

func nonReachoutServerErr(code string) error {
	return errors.Join(whatsmeow.ErrServerReturnedError, errors.New("server returned error "+code))
}

func TestIsReachoutTimelockError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"wrapped 463", reachoutErr(), true},
		{"wrapped non-463", nonReachoutServerErr("500"), false},
		{"unrelated error", errors.New("network unreachable"), false},
		{"bare 463 string without sentinel", errors.New("error 463"), false},
		{"sentinel but no 463", nonReachoutServerErr("400"), false},
		{"superstring 4631 must not match", nonReachoutServerErr("4631"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsReachoutTimelockError(tt.err); got != tt.want {
				t.Fatalf("IsReachoutTimelockError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestReachoutCooldown(t *testing.T) {
	ctx := context.Background()
	jid := types.NewJID("6281234567890", types.DefaultUserServer)
	other := types.NewJID("6289999999999", types.DefaultUserServer)

	if got := ReachoutCooldownRemaining(ctx, jid); got != 0 {
		t.Fatalf("unknown recipient should not be in cooldown, got %v", got)
	}

	MarkReachoutTimelocked(ctx, jid)
	defer ClearReachoutTimelock(ctx, jid)

	if got := ReachoutCooldownRemaining(ctx, jid); got <= 0 || got > reachoutCooldown {
		t.Fatalf("expected active cooldown within %v, got %v", reachoutCooldown, got)
	}
	// Device-scoped keys must not leak across recipients.
	if got := ReachoutCooldownRemaining(ctx, other); got != 0 {
		t.Fatalf("other recipient should not be blocked, got %v", got)
	}
	// A recipient with an AD suffix is the same conversation.
	withDevice := jid
	withDevice.Device = 3
	if got := ReachoutCooldownRemaining(ctx, withDevice); got <= 0 {
		t.Fatalf("AD-suffixed JID should share the cooldown, got %v", got)
	}

	ClearReachoutTimelock(ctx, jid)
	if got := ReachoutCooldownRemaining(ctx, jid); got != 0 {
		t.Fatalf("cooldown should be cleared, got %v", got)
	}

	// An expired entry reads as no cooldown and is pruned.
	reachoutBlocked.Store(reachoutKey(ctx, jid), time.Now().Add(-time.Minute))
	if got := ReachoutCooldownRemaining(ctx, jid); got != 0 {
		t.Fatalf("expired cooldown should read as 0, got %v", got)
	}
	if _, ok := reachoutBlocked.Load(reachoutKey(ctx, jid)); ok {
		t.Fatal("expired cooldown entry should have been pruned")
	}
}

func TestReserveColdSendSlotSpacesSends(t *testing.T) {
	device := "spacing-test"
	defer func() {
		coldSendLock.Lock()
		delete(coldSendNext, device)
		coldSendLock.Unlock()
	}()

	const interval = 5 * time.Second

	// First cold send goes out immediately, the next ones queue behind it.
	for i, want := range []time.Duration{0, interval, 2 * interval} {
		wait, ok := ReserveColdSendSlot(device, interval, time.Minute)
		if !ok {
			t.Fatalf("send %d: slot unexpectedly refused", i)
		}
		// Allow for the clock advancing between reservations.
		if wait > want || wait < want-time.Second {
			t.Fatalf("send %d: wait = %v, want ~%v", i, wait, want)
		}
	}

	// Queue is now ~15s deep, so a caller that will not wait that long is
	// refused instead of hanging, and the refusal must not consume a slot.
	wait, ok := ReserveColdSendSlot(device, interval, time.Second)
	if ok {
		t.Fatalf("expected refusal when queue (%v) exceeds maxWait", wait)
	}
	if next, again := ReserveColdSendSlot(device, interval, time.Minute); !again || next > 3*interval {
		t.Fatalf("refused reservation consumed a slot: next wait = %v", next)
	}

	// Pacing disabled means never wait.
	if wait, ok := ReserveColdSendSlot(device, 0, 0); !ok || wait != 0 {
		t.Fatalf("interval 0 should disable pacing, got wait=%v ok=%v", wait, ok)
	}
}

func TestIsColdReachoutTarget(t *testing.T) {
	tests := []struct {
		name string
		jid  types.JID
		want bool
	}{
		{"user", types.NewJID("6281234567890", types.DefaultUserServer), true},
		{"lid", types.NewJID("123456789", types.HiddenUserServer), true},
		{"group", types.NewJID("120363000000000000", types.GroupServer), false},
		{"newsletter", types.NewJID("120363000000000000", types.NewsletterServer), false},
		{"status broadcast", types.StatusBroadcastJID, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsColdReachoutTarget(tt.jid); got != tt.want {
				t.Fatalf("IsColdReachoutTarget(%s) = %v, want %v", tt.jid, got, tt.want)
			}
		})
	}
}
