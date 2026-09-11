package whatsapp

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// ReachoutTimelockCode is WhatsApp server error 463 (NackCallerReachoutTimelocked,
// "reach-out timelock"): the server-side anti-spam restriction on starting new
// chats.
const ReachoutTimelockCode = 463

// serverErrorCodePattern extracts the numeric code whatsmeow embeds when the
// server nacks a send: fmt.Errorf("%w %d", ErrServerReturnedError, code), which
// callers then wrap further ("failed to send message node: ..."). Matching the
// number rather than the string suffix keeps this working if anything is
// appended after it.
var serverErrorCodePattern = regexp.MustCompile(`server returned error (\d+)`)

// ServerErrorCode returns the numeric error code WhatsApp rejected a send with.
// ok is false for any error that is not a server nack, so an unrelated failure
// (network, encryption, validation) is never reported as a WhatsApp code.
func ServerErrorCode(err error) (code int, ok bool) {
	if err == nil || !errors.Is(err, whatsmeow.ErrServerReturnedError) {
		return 0, false
	}
	match := serverErrorCodePattern.FindStringSubmatch(err.Error())
	if match == nil {
		return 0, false
	}
	code, convErr := strconv.Atoi(match[1])
	return code, convErr == nil
}

// IsReachoutTimelockError reports whether err is WhatsApp server error 463.
// It surfaces when the recipient has no prior conversation with this account
// (no stored privacy token) or the sending account is temporarily restricted
// from reaching new contacts. It is enforced by WhatsApp and cannot be bypassed
// by the client — the trusted-contact token whatsmeow attaches on the outbound
// path can only originate from the recipient's side (an inbound message, a
// privacy_token notification, or history sync).
func IsReachoutTimelockError(err error) bool {
	code, ok := ServerErrorCode(err)
	return ok && code == ReachoutTimelockCode
}

// reachoutCooldown is how long a recipient stays locally refused after WhatsApp
// rejected a send to them with 463. The timelock is enforced server-side and
// clears on WhatsApp's schedule, so retrying immediately cannot succeed — it
// only adds more rejected reachouts to the account, which is what escalates a
// timelock into a ban. Refusing the retry locally keeps those attempts off the
// wire.
const reachoutCooldown = time.Hour

// ponytail: process-local map, so the cooldown resets on restart and is not
// shared between replicas. Persist it in chat storage if that matters.
var reachoutBlocked sync.Map // string key -> time.Time expiry

// DeviceKey identifies a device for the per-account state in this file. Every
// reader and writer must derive it the same way or the state silently misses:
// the account JID when the device is paired, falling back to the slot id.
func DeviceKey(instance *DeviceInstance) string {
	if instance == nil {
		return ""
	}
	if jid := instance.JID(); jid != "" {
		return jid
	}
	return instance.ID()
}

// DeviceKeyFromContext is DeviceKey for the device stored in ctx.
func DeviceKeyFromContext(ctx context.Context) string {
	instance, _ := DeviceFromContext(ctx)
	return DeviceKey(instance)
}

// reachoutKey scopes a recipient to the device that tried to reach them, so a
// timelock on one linked account does not block the same number on another.
func reachoutKey(ctx context.Context, jid types.JID) string {
	return DeviceKeyFromContext(ctx) + "|" + jid.ToNonAD().String()
}

// MarkReachoutTimelocked records that WhatsApp rejected a send to jid with 463.
func MarkReachoutTimelocked(ctx context.Context, jid types.JID) {
	reachoutBlocked.Store(reachoutKey(ctx, jid), time.Now().Add(reachoutCooldown))
}

// ReachoutCooldownRemaining returns how much longer sends to jid are refused
// locally, or 0 when the recipient is not (or no longer) in cooldown.
func ReachoutCooldownRemaining(ctx context.Context, jid types.JID) time.Duration {
	key := reachoutKey(ctx, jid)
	value, ok := reachoutBlocked.Load(key)
	if !ok {
		return 0
	}
	remaining := time.Until(value.(time.Time))
	if remaining <= 0 {
		reachoutBlocked.Delete(key)
		return 0
	}
	return remaining
}

// ClearReachoutTimelock drops the cooldown for jid. An inbound message from the
// recipient is what lifts the timelock (it is how the trusted-contact token
// reaches this account), so the next send must be allowed through immediately.
func ClearReachoutTimelock(ctx context.Context, jid types.JID) {
	reachoutBlocked.Delete(reachoutKey(ctx, jid))
}

// tcTokenAssumedLifetime is how long a stored trusted-contact token is treated
// as still good. whatsmeow's real window is a rolling 21-28 days (4 x 7-day
// buckets, see its tctoken.go) and the exact cutoff is unexported, so the
// conservative end is used: at worst a still-valid contact is treated as cold,
// which only costs one throttle slot and a log line.
const tcTokenAssumedLifetime = 21 * 24 * time.Hour

// TrustedContactTokenTime returns when this account received the trusted-contact
// token for jid. Without one, a send to jid is a cold reach-out — the case
// WhatsApp rejects with error 463. The store query already falls back between
// LID and phone-number forms, so either form of jid works.
func TrustedContactTokenTime(ctx context.Context, client *whatsmeow.Client, jid types.JID) (time.Time, bool) {
	if client == nil || client.Store == nil || client.Store.PrivacyTokens == nil {
		return time.Time{}, false
	}
	token, err := client.Store.PrivacyTokens.GetPrivacyToken(ctx, jid.ToNonAD())
	if err != nil {
		log.Warnf("Failed to read trusted-contact token for %s: %v", jid, err)
		return time.Time{}, false
	}
	if token == nil || len(token.Token) == 0 || time.Since(token.Timestamp) >= tcTokenAssumedLifetime {
		return time.Time{}, false
	}
	return token.Timestamp, true
}

// HasTrustedContactToken reports whether jid is a warm contact for this account.
func HasTrustedContactToken(ctx context.Context, client *whatsmeow.Client, jid types.JID) bool {
	_, ok := TrustedContactTokenTime(ctx, client, jid)
	return ok
}

// IsColdReachoutTarget reports whether a 463 can apply to this recipient at all.
// Groups, newsletters and status broadcasts are not reach-outs, so they must
// never be throttled as one.
func IsColdReachoutTarget(jid types.JID) bool {
	jid = jid.ToNonAD()
	return (jid.Server == types.DefaultUserServer || jid.Server == types.HiddenUserServer) &&
		jid != types.StatusBroadcastJID &&
		!jid.IsBot()
}

// ponytail: in-process spacing only — one replica per account assumed. Move the
// slot table to shared storage if gowa is ever run multi-replica per device.
var (
	coldSendLock sync.Mutex
	coldSendNext = map[string]time.Time{}
)

// ReserveColdSendSlot paces cold reach-outs per device: it returns how long the
// caller must wait before sending, having reserved that slot. Bursts of cold
// reach-outs are what turn an occasional 463 into an account-wide timelock, so
// they are spread out instead of fired back to back. ok is false when the queue
// is already longer than maxWait — the caller should reject the send rather
// than hold the request open, and no slot is consumed.
func ReserveColdSendSlot(deviceID string, interval, maxWait time.Duration) (wait time.Duration, ok bool) {
	if interval <= 0 {
		return 0, true
	}

	coldSendLock.Lock()
	defer coldSendLock.Unlock()

	now := time.Now()
	slot := coldSendNext[deviceID]
	if slot.Before(now) {
		slot = now
	}
	if wait = slot.Sub(now); wait > maxWait {
		return wait, false
	}
	coldSendNext[deviceID] = slot.Add(interval)
	return wait, true
}

// ColdSendQueueDepth reports how long the next cold reach-out would have to wait
// without reserving the slot, so a read-only caller (a preflight check) does not
// consume pacing capacity meant for real sends.
func ColdSendQueueDepth(deviceID string) time.Duration {
	coldSendLock.Lock()
	defer coldSendLock.Unlock()

	if wait := time.Until(coldSendNext[deviceID]); wait > 0 {
		return wait
	}
	return 0
}
