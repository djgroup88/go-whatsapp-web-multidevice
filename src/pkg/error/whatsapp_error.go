package error

import (
	"fmt"
	"net/http"
)

type InvalidJID string

// Error for complying the error interface
func (e InvalidJID) Error() string {
	return string(e)
}

// ErrCode will return the error code based on the error data type
func (e InvalidJID) ErrCode() string {
	return "INVALID_JID"
}

// StatusCode will return the HTTP status code based on the error data type
func (e InvalidJID) StatusCode() int {
	return http.StatusBadRequest
}

type WebhookError string

// Error for complying the error interface
func (e WebhookError) Error() string {
	return string(e)
}

// ErrCode will return the error code based on the error data type
func (e WebhookError) ErrCode() string {
	return "WEBHOOK_ERROR"
}

// StatusCode will return the HTTP status code based on the error data type
func (e WebhookError) StatusCode() int {
	return http.StatusInternalServerError
}

// WaCliError means the addressed device has no live WhatsApp session. An unknown
// device id never reaches here — the device middleware answers 404 first — so this
// is always a device that exists but is not linked, was logged out, or has not
// finished connecting yet.
type WaCliError string

// Error for complying the error interface
func (e WaCliError) Error() string {
	return string(e)
}

// ErrCode will return the error code based on the error data type.
// Kept as INVALID_WA_CLI: API clients branch on this value.
func (e WaCliError) ErrCode() string {
	return "INVALID_WA_CLI"
}

// StatusCode will return the HTTP status code based on the error data type.
// 503, not 500: nothing failed inside gowa. The device is simply not connected
// yet, which is temporary and resolves once it is linked — and a caller that
// retries 5xx keeps working, since 503 is still a server-class status.
func (e WaCliError) StatusCode() int {
	return http.StatusServiceUnavailable
}

type WaUploadMediaError string

// Error for complying the error interface
func (e WaUploadMediaError) Error() string {
	return string(e)
}

// ErrCode will return the error code based on the error data type
func (e WaUploadMediaError) ErrCode() string {
	return "UPLOAD_MEDIA_ERROR"
}

// StatusCode will return the HTTP status code based on the error data type
func (e WaUploadMediaError) StatusCode() int {
	return http.StatusInternalServerError
}

// WaServerError carries a send rejection that WhatsApp answered with a numeric
// code. whatsmeow passes those codes through verbatim, so without this the code
// is buried inside a 500's message text and an API client can only show the bare
// number. Keeping it in ErrCode lets a caller branch on it.
type WaServerError struct {
	Code int
	Err  error
}

// Error for complying the error interface
func (e WaServerError) Error() string {
	return fmt.Sprintf("WhatsApp rejected this send with server error %d. The code comes from WhatsApp, not from gowa. A linked device only knows the conversations and trusted-contact tokens that reached its own session, so a chat that exists on your phone is not necessarily known here. Check the gowa log for the full error and whether the contact was cold or warm: %v", e.Code, e.Err)
}

// ErrCode will return the error code based on the error data type
func (e WaServerError) ErrCode() string {
	return fmt.Sprintf("WA_SERVER_ERROR_%d", e.Code)
}

// StatusCode will return the HTTP status code based on the error data type
func (e WaServerError) StatusCode() int {
	return http.StatusBadGateway
}

// Unwrap keeps errors.Is/As working against the original whatsmeow error.
func (e WaServerError) Unwrap() error {
	return e.Err
}

type WaReachoutTimelockError string

// Error for complying the error interface
func (e WaReachoutTimelockError) Error() string {
	return string(e)
}

// ErrCode will return the error code based on the error data type
func (e WaReachoutTimelockError) ErrCode() string {
	return "WA_REACHOUT_TIMELOCK"
}

// StatusCode will return the HTTP status code based on the error data type
func (e WaReachoutTimelockError) StatusCode() int {
	return http.StatusTooManyRequests
}

const (
	ErrUserNotRegistered = InvalidJID("user is not registered")
	// The three ways a device can have no usable WhatsApp session. They share one
	// code and one status on purpose: to an API client they are one condition with
	// one remedy (link the device, or send from another one), and splitting them
	// across 401/503 meant a caller could only recognise a third of the cases.
	// Only the prose differs, so an operator can still tell them apart in a log.
	ErrWaCLI          = WaCliError("This device has no active WhatsApp session, so nothing can be sent from it. Either it was never linked, it was logged out from the phone, or it is still connecting after a restart. Check GET /app/devices/{device_id}/status, and link it again via GET /app/devices/{device_id}/login (scan the QR) if it is logged out. Make sure the X-Device-Id you send belongs to a linked device: an empty slot answers this on every request.")
	ErrWaNotConnected = WaCliError("This device is not connected to WhatsApp right now. It may still be reconnecting after a restart or a dropped link, which usually resolves on its own within seconds, so this request is safe to retry. Check GET /app/devices/{device_id}/status, or force it with POST /app/devices/{device_id}/reconnect.")
	ErrWaNotLoggedIn  = WaCliError("This device is connected to WhatsApp but has no logged-in session: it was never linked, or it was logged out from the phone. Link it again via GET /app/devices/{device_id}/login and scan the QR. Retrying without linking it will keep failing.")

	ErrWaColdSendBacklog  = WaReachoutTimelockError("Too many queued sends to contacts with no existing conversation. gowa paces these to avoid WhatsApp error 463 (reach-out timelock) and the account restriction that follows it. Retry this send later, spread your sends to new contacts over time, or lower the pacing with --cold-send-interval / WHATSAPP_COLD_SEND_INTERVAL (0 disables it).")
	ErrWaReachoutTimelock = WaReachoutTimelockError("WhatsApp rejected this send with error 463 (reach-out timelock). This is WhatsApp's server-side anti-spam restriction on starting new chats and cannot be bypassed by the API. It usually means there is no prior conversation with this recipient, or the sending account is temporarily restricted from reaching new contacts. To get through: ask the recipient to message you first, reply within an already-established conversation, or open and send the chat once from the phone linked to this session, then reduce your sending rate and wait for the restriction to clear. Newly-linked or low-activity numbers are affected most.")
)
