package usecase

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

type replyMessageRepo struct {
	domainChatStorage.IChatStorageRepository
	message     *domainChatStorage.Message
	err         error
	gotID       string
	gotDeviceID string
}

func (r *replyMessageRepo) GetMessageByIDAndDevice(deviceID, id string) (*domainChatStorage.Message, error) {
	r.gotDeviceID = deviceID
	r.gotID = id
	return r.message, r.err
}

func TestWithoutCancelPreservesDeviceContext(t *testing.T) {
	deviceID := "6289605618749@s.whatsapp.net"
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, nil))

	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()

	storeCtx := context.WithoutCancel(cancelledCtx)
	inst, ok := whatsapp.DeviceFromContext(storeCtx)
	if !ok || inst == nil {
		t.Fatal("expected device instance to remain in detached context")
	}
	if got := inst.ID(); got != deviceID {
		t.Fatalf("expected device id %q, got %q", deviceID, got)
	}
}

func TestResolveDocumentMIME(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		wantMIME string
	}{
		{
			name:     "Docx",
			filename: "document.docx",
			wantMIME: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		},
		{
			name:     "Xlsx",
			filename: "spreadsheet.xlsx",
			wantMIME: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		},
		{
			name:     "Pptx",
			filename: "presentation.pptx",
			wantMIME: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		},
		{
			name:     "Zip",
			filename: "archive.zip",
			wantMIME: "application/zip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveDocumentMIME(tt.filename, []byte("dummy"))
			if got != tt.wantMIME {
				t.Fatalf("resolveDocumentMIME() = %q, want %q", got, tt.wantMIME)
			}
		})
	}
}

func TestBuildLinkMessageText(t *testing.T) {
	tests := []struct {
		name    string
		caption string
		link    string
		want    string
	}{
		{
			name: "returns link when caption is empty",
			link: "https://example.com",
			want: "https://example.com",
		},
		{
			name:    "joins caption and link with newline",
			caption: "Check this out",
			link:    "https://example.com",
			want:    "Check this out\nhttps://example.com",
		},
		{
			name:    "ignores blank caption",
			caption: "   ",
			link:    "https://example.com",
			want:    "https://example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildLinkMessageText(tt.caption, tt.link)
			if got != tt.want {
				t.Fatalf("buildLinkMessageText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeSendErrorMapsReachoutTimelock(t *testing.T) {
	err := normalizeSendError(errors.Join(whatsmeow.ErrServerReturnedError, errors.New("server returned error 463")))

	genericErr, ok := err.(pkgError.GenericError)
	if !ok {
		t.Fatalf("expected generic error, got %T", err)
	}
	if got := genericErr.ErrCode(); got != "WA_REACHOUT_TIMELOCK" {
		t.Fatalf("expected WA_REACHOUT_TIMELOCK code, got %q", got)
	}
	if got := genericErr.StatusCode(); got != http.StatusTooManyRequests {
		t.Fatalf("expected status %d, got %d", http.StatusTooManyRequests, got)
	}
	if got := genericErr.Error(); got != string(pkgError.ErrWaReachoutTimelock) {
		t.Fatalf("unexpected error message: %q", got)
	}
}

func TestMergeReplyContextAddsQuoteFields(t *testing.T) {
	replyID := "3EB089B9D6ADD58153C561"
	repo := &replyMessageRepo{
		message: &domainChatStorage.Message{
			Sender:  "628123456789@s.whatsapp.net",
			Content: "quoted message body",
		},
	}
	service := serviceSend{chatStorageRepo: repo}
	contextInfo := &waE2E.ContextInfo{}

	deviceID := "6289605618749@s.whatsapp.net"
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, nil))
	got := service.mergeReplyContext(ctx, contextInfo, &replyID)

	if got != contextInfo {
		t.Fatal("expected existing context info to be reused")
	}
	if repo.gotID != replyID {
		t.Fatalf("expected lookup for reply ID %q, got %q", replyID, repo.gotID)
	}
	if repo.gotDeviceID != deviceID {
		t.Fatalf("expected device-scoped lookup for %q, got %q", deviceID, repo.gotDeviceID)
	}
	if got.GetStanzaID() != replyID {
		t.Fatalf("expected stanza ID %q, got %q", replyID, got.GetStanzaID())
	}
	if got.GetParticipant() != "628123456789@s.whatsapp.net" {
		t.Fatalf("unexpected participant: %q", got.GetParticipant())
	}
	if got.GetQuotedMessage().GetConversation() != "quoted message body" {
		t.Fatalf("unexpected quoted body: %q", got.GetQuotedMessage().GetConversation())
	}
}

func TestMergeReplyContextPreservesExistingContext(t *testing.T) {
	replyID := "3EB089B9D6ADD58153C561"
	repo := &replyMessageRepo{
		message: &domainChatStorage.Message{
			Sender:  "628123456789@s.whatsapp.net",
			Content: "quoted message body",
		},
	}
	service := serviceSend{chatStorageRepo: repo}
	contextInfo := &waE2E.ContextInfo{
		IsForwarded:     proto.Bool(true),
		ForwardingScore: proto.Uint32(100),
		Expiration:      proto.Uint32(3600),
		MentionedJID:    []string{"628999999999@s.whatsapp.net"},
	}

	got := service.mergeReplyContext(context.Background(), contextInfo, &replyID)

	if !got.GetIsForwarded() {
		t.Fatal("expected forwarded flag to be preserved")
	}
	if got.GetForwardingScore() != 100 {
		t.Fatalf("expected forwarding score 100, got %d", got.GetForwardingScore())
	}
	if got.GetExpiration() != 3600 {
		t.Fatalf("expected expiration 3600, got %d", got.GetExpiration())
	}
	if len(got.GetMentionedJID()) != 1 || got.GetMentionedJID()[0] != "628999999999@s.whatsapp.net" {
		t.Fatalf("expected mentioned JIDs to be preserved, got %#v", got.GetMentionedJID())
	}
	if got.GetQuotedMessage().GetConversation() != "quoted message body" {
		t.Fatalf("unexpected quoted body: %q", got.GetQuotedMessage().GetConversation())
	}
}

func TestMergeReplyContextLeavesExistingContextWhenReplyUnavailable(t *testing.T) {
	replyID := "3EB089B9D6ADD58153C561"

	tests := []struct {
		name    string
		replyID *string
		message *domainChatStorage.Message
		err     error
	}{
		{
			name: "nil reply ID",
		},
		{
			name:    "empty reply ID",
			replyID: proto.String(""),
		},
		{
			name:    "message not found",
			replyID: &replyID,
		},
		{
			name:    "lookup error",
			replyID: &replyID,
			err:     errors.New("storage unavailable"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contextInfo := &waE2E.ContextInfo{Expiration: proto.Uint32(3600)}
			service := serviceSend{chatStorageRepo: &replyMessageRepo{
				message: tt.message,
				err:     tt.err,
			}}

			got := service.mergeReplyContext(context.Background(), contextInfo, tt.replyID)

			if got != contextInfo {
				t.Fatal("expected existing context info to be reused")
			}
			if got.GetExpiration() != 3600 {
				t.Fatalf("expected expiration to remain 3600, got %d", got.GetExpiration())
			}
			if got.GetStanzaID() != "" {
				t.Fatalf("expected no stanza ID, got %q", got.GetStanzaID())
			}
			if got.GetQuotedMessage() != nil {
				t.Fatalf("expected no quoted message, got %#v", got.GetQuotedMessage())
			}
		})
	}
}

func TestSendForwardMessageNotFound(t *testing.T) {
	repo := &replyMessageRepo{}
	service := serviceSend{chatStorageRepo: repo}
	deviceID := "6289605618749@s.whatsapp.net"
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, nil))

	_, err := service.SendForward(ctx, domainSend.ForwardRequest{
		MessageID: "missing-id",
		Phone:     "628123456789@s.whatsapp.net",
	})
	if err == nil {
		t.Fatal("expected error for missing message")
	}
	if repo.gotID != "missing-id" {
		t.Fatalf("expected lookup for missing-id, got %q", repo.gotID)
	}
	if repo.gotDeviceID != deviceID {
		t.Fatalf("expected device-scoped lookup for %q, got %q", deviceID, repo.gotDeviceID)
	}
}

func TestForwardDurationOptionExplicitZero(t *testing.T) {
	zero := 0
	got := forwardDurationOption(serviceSend{}, domainSend.ForwardRequest{
		Phone:    "628123456789@s.whatsapp.net",
		Duration: &zero,
	})
	if got == nil || *got != 0 {
		t.Fatalf("expected explicit duration 0 to be honored, got %v", got)
	}
}

func TestSendForwardUnsupportedType(t *testing.T) {
	repo := &replyMessageRepo{
		message: &domainChatStorage.Message{
			MediaType: "call",
			Content:   "incoming call",
		},
	}
	service := serviceSend{chatStorageRepo: repo}
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance("6289605618749@s.whatsapp.net", nil, nil))

	_, err := service.SendForward(ctx, domainSend.ForwardRequest{
		MessageID: "call-msg-id",
		Phone:     "628123456789@s.whatsapp.net",
	})
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
	genericErr, ok := err.(pkgError.GenericError)
	if !ok {
		t.Fatalf("expected validation error, got %T: %v", err, err)
	}
	if genericErr.Error() != utils.ErrUnsupportedForwardType {
		t.Fatalf("error = %q, want %q", genericErr.Error(), utils.ErrUnsupportedForwardType)
	}
}

func TestNormalizeSendErrorSurfacesServerCode(t *testing.T) {
	err := normalizeSendError(fmt.Errorf("failed to send message node: %w 426", whatsmeow.ErrServerReturnedError))

	genericErr, ok := err.(pkgError.GenericError)
	if !ok {
		t.Fatalf("expected generic error, got %T", err)
	}
	if got := genericErr.ErrCode(); got != "WA_SERVER_ERROR_426" {
		t.Fatalf("expected WA_SERVER_ERROR_426, got %q", got)
	}
	if got := genericErr.StatusCode(); got != http.StatusBadGateway {
		t.Fatalf("expected status %d, got %d", http.StatusBadGateway, got)
	}
	if !errors.Is(err, whatsmeow.ErrServerReturnedError) {
		t.Fatal("wrapped error must still unwrap to the whatsmeow sentinel")
	}
	// A failure that is not a server nack must pass through untouched.
	plain := errors.New("dial tcp: connection refused")
	if got := normalizeSendError(plain); got != plain {
		t.Fatalf("non-server error was rewritten: %v", got)
	}
}

// preflightCtx builds a device context whose client has no privacy token store,
// so every recipient reads as cold — the case the endpoint exists to report.
func preflightCtx(deviceID string) context.Context {
	// The device key is the account JID, so each test needs its own account.
	accountJID := types.NewJID(deviceID, types.DefaultUserServer)
	client := &whatsmeow.Client{Store: &store.Device{ID: &accountJID}}
	return whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, client, nil))
}

func TestPreflightReportsColdRecipient(t *testing.T) {
	service := serviceSend{}
	ctx := preflightCtx("6280000000001")

	got, err := service.Preflight(ctx, domainSend.PreflightRequest{Phone: "6281111111111"})
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if got.Warm {
		t.Fatal("recipient without a stored token must read as cold")
	}
	if !got.Sendable {
		t.Fatalf("a cold recipient is still worth attempting, got sendable=false: %s", got.Reason)
	}
	if got.JID != "6281111111111@s.whatsapp.net" {
		t.Fatalf("unexpected JID: %s", got.JID)
	}
}

func TestPreflightReportsRecipientCooldown(t *testing.T) {
	service := serviceSend{}
	ctx := preflightCtx("6280000000002")
	recipient := types.NewJID("6282222222222", types.DefaultUserServer)

	whatsapp.MarkReachoutTimelocked(ctx, recipient)
	defer whatsapp.ClearReachoutTimelock(ctx, recipient)

	got, err := service.Preflight(ctx, domainSend.PreflightRequest{Phone: "6282222222222"})
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if got.Sendable {
		t.Fatal("a recipient in cooldown must not be reported as sendable")
	}
	if got.CooldownSeconds <= 0 {
		t.Fatalf("expected a remaining cooldown, got %d", got.CooldownSeconds)
	}
}

// timelockRepo stubs only the reach-out timelock read; everything else is unused
// by Preflight.
type timelockRepo struct {
	domainChatStorage.IChatStorageRepository
	endsAtUnix int64
}

func (r *timelockRepo) GetReachoutTimelock(string) (string, int64, error) {
	return "", r.endsAtUnix, nil
}

func TestPreflightReportsAccountTimelock(t *testing.T) {
	endsAt := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	service := serviceSend{chatStorageRepo: &timelockRepo{endsAtUnix: endsAt.Unix()}}
	ctx := preflightCtx("6280000000003")

	got, err := service.Preflight(ctx, domainSend.PreflightRequest{Phone: "6283333333333"})
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if got.Sendable {
		t.Fatal("no cold send is sendable while the account is timelocked")
	}
	if got.AccountTimelockEndsAt == nil || !got.AccountTimelockEndsAt.Equal(endsAt) {
		t.Fatalf("expected the timelock end time to be reported, got %v", got.AccountTimelockEndsAt)
	}
}

func TestPreflightIgnoresExpiredAccountTimelock(t *testing.T) {
	// WhatsApp does not always announce that a lock lifted, so a past end time
	// must read as no restriction rather than blocking sends forever.
	expired := time.Now().Add(-time.Minute)
	service := serviceSend{chatStorageRepo: &timelockRepo{endsAtUnix: expired.Unix()}}
	ctx := preflightCtx("6280000000005")

	got, err := service.Preflight(ctx, domainSend.PreflightRequest{Phone: "6283333333334"})
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if !got.Sendable || got.AccountTimelockEndsAt != nil {
		t.Fatalf("expired timelock must not gate sends: %+v", got)
	}
}

func TestPreflightSkipsGroups(t *testing.T) {
	// A group is not a reach-out, so even an active account timelock is irrelevant.
	endsAt := time.Now().Add(time.Hour)
	service := serviceSend{chatStorageRepo: &timelockRepo{endsAtUnix: endsAt.Unix()}}
	ctx := preflightCtx("6280000000004")

	got, err := service.Preflight(ctx, domainSend.PreflightRequest{Phone: "120363000000000000@g.us"})
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if !got.Sendable || got.AccountTimelockEndsAt != nil {
		t.Fatalf("group send must not be gated by the reach-out timelock: %+v", got)
	}
}

func TestPreflightReportsTimelockAlongsideCooldown(t *testing.T) {
	// The account-wide lock is the root cause; a per-recipient cooldown must not
	// hide it, or the operator retries other numbers and keeps digging.
	endsAt := time.Now().Add(3 * time.Hour).Truncate(time.Second)
	service := serviceSend{chatStorageRepo: &timelockRepo{endsAtUnix: endsAt.Unix()}}
	ctx := preflightCtx("6280000000006")
	recipient := types.NewJID("6284444444444", types.DefaultUserServer)

	whatsapp.MarkReachoutTimelocked(ctx, recipient)
	defer whatsapp.ClearReachoutTimelock(ctx, recipient)

	got, err := service.Preflight(ctx, domainSend.PreflightRequest{Phone: "6284444444444"})
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if got.CooldownSeconds <= 0 {
		t.Fatal("the recipient cooldown must still be reported")
	}
	if got.AccountTimelockEndsAt == nil || !got.AccountTimelockEndsAt.Equal(endsAt) {
		t.Fatalf("the account timelock must be reported too, got %v", got.AccountTimelockEndsAt)
	}
	if !strings.Contains(got.Reason, "whole account") {
		t.Fatalf("reason should name the account-wide restriction, got: %s", got.Reason)
	}
}

func TestPreflightWithoutPhoneAnswersAboutTheDevice(t *testing.T) {
	endsAt := time.Now().Add(4 * time.Hour).Truncate(time.Second)
	service := serviceSend{chatStorageRepo: &timelockRepo{endsAtUnix: endsAt.Unix()}}

	got, err := service.Preflight(preflightCtx("6280000000007"), domainSend.PreflightRequest{})
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if got.Scope != "device" {
		t.Fatalf("Scope = %q, want device", got.Scope)
	}
	if got.Sendable {
		t.Fatal("a timelocked account cannot start new chats")
	}
	if got.AccountTimelockEndsAt == nil || !got.AccountTimelockEndsAt.Equal(endsAt) {
		t.Fatalf("timelock end time not reported: %v", got.AccountTimelockEndsAt)
	}
	// Recipient-only fields must stay empty so a caller cannot read them as facts.
	if got.Phone != "" || got.JID != "" || got.Warm || got.CooldownSeconds != 0 {
		t.Fatalf("recipient fields leaked into a device-scoped answer: %+v", got)
	}
}

func TestPreflightWithoutPhoneWhenAccountIsFree(t *testing.T) {
	service := serviceSend{chatStorageRepo: &timelockRepo{}}

	got, err := service.Preflight(preflightCtx("6280000000008"), domainSend.PreflightRequest{})
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if !got.Sendable || got.AccountTimelockEndsAt != nil {
		t.Fatalf("an unrestricted account must read as sendable: %+v", got)
	}
	if got.Scope != "device" {
		t.Fatalf("Scope = %q, want device", got.Scope)
	}
}

func TestPreflightReportsTimelockEvenForWarmRecipient(t *testing.T) {
	// One refresh of a contact must also tell the caller the account's state, or a
	// client is forced into a second request per contact just to learn it.
	endsAt := time.Now().Add(time.Hour).Truncate(time.Second)
	service := serviceSend{chatStorageRepo: &timelockRepo{endsAtUnix: endsAt.Unix()}}
	ctx := preflightCtx("6280000000009")
	recipient := types.NewJID("6285555555555", types.DefaultUserServer)

	// Warm contacts are unaffected by the restriction, so it must not block them.
	whatsapp.MarkReachoutTimelocked(ctx, recipient)
	whatsapp.ClearReachoutTimelock(ctx, recipient)

	got, err := service.Preflight(ctx, domainSend.PreflightRequest{Phone: "6285555555555"})
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if got.AccountTimelockEndsAt == nil || !got.AccountTimelockEndsAt.Equal(endsAt) {
		t.Fatalf("account timelock must always be reported, got %v", got.AccountTimelockEndsAt)
	}
	// Cold recipient on the same account is the one that is blocked.
	if got.Warm && !got.Sendable {
		t.Fatal("a warm recipient stays sendable while the account is timelocked")
	}
}
