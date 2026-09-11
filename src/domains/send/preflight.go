package send

import "time"

// PreflightRequest asks whether a send to Phone is likely to be accepted by
// WhatsApp, without actually sending anything. Phone is optional: omitting it
// asks only about the device, which is what a caller wants when it has no
// recipient in hand — or, as with a CRM that deliberately stores no phone
// numbers, cannot produce one cheaply.
type PreflightRequest struct {
	Phone string `json:"phone" form:"phone" query:"phone"`
}

// PreflightResponse describes what this session knows about a recipient before
// a send is attempted. It is derived entirely from local state — no query is
// sent to WhatsApp, so calling it costs nothing against the account.
type PreflightResponse struct {
	// Scope is "recipient" when a phone was given, "device" when it was not. It
	// tells a caller which fields carry meaning: Warm and the cooldown are about
	// one recipient and stay zero on a device-scoped answer.
	Scope string `json:"scope"`
	Phone string `json:"phone,omitempty"`
	JID   string `json:"jid,omitempty"`
	// Warm is true when this session holds a trusted-contact token for the
	// recipient, which is what lets a message through WhatsApp's restriction on
	// starting new chats. Cold recipients are the ones rejected with error 463.
	Warm bool `json:"warm"`
	// TokenReceivedAt is when the trusted-contact token arrived (warm only).
	TokenReceivedAt *time.Time `json:"token_received_at,omitempty"`
	// Sendable is the bottom line: false means a send right now is expected to
	// fail or be refused locally.
	Sendable bool   `json:"sendable"`
	Reason   string `json:"reason"`
	// CooldownSeconds is how long this recipient stays refused locally after a
	// recent 463 rejection.
	CooldownSeconds int `json:"cooldown_seconds,omitempty"`
	// QueueWaitSeconds is how long a cold send would currently wait for a pacing
	// slot. Groups and warm contacts are never paced.
	QueueWaitSeconds int `json:"queue_wait_seconds,omitempty"`
	// AccountTimelockEndsAt is set while WhatsApp has restricted this whole
	// account from starting new chats. No cold send succeeds until then.
	AccountTimelockEndsAt *time.Time `json:"account_timelock_ends_at,omitempty"`
}
