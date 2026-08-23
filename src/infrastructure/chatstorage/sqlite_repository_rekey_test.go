package chatstorage

import (
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

// Logout switches a slot's chat partition from its bare JID to the slot id (and
// re-pairing switches it back). RekeyDeviceData must carry chats, messages, edits and
// reactions across, keep rows that already exist under the target key, and leave
// nothing behind under the source key so a purge by either key sweeps everything.
func TestSQLiteRepositoryRekeyDeviceDataMovesChatHistory(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	const (
		jidKey  = "628111@s.whatsapp.net"
		slotKey = "slot-uuid"
		chatJID = "628222@s.whatsapp.net"
	)
	now := time.Now()

	mustStore := func(deviceID, msgID, content string) {
		t.Helper()
		if err := repo.StoreChat(&domainChatStorage.Chat{DeviceID: deviceID, JID: chatJID, Name: "c-" + deviceID, LastMessageTime: now}); err != nil {
			t.Fatalf("store chat: %v", err)
		}
		if err := repo.StoreMessage(&domainChatStorage.Message{ID: msgID, ChatJID: chatJID, DeviceID: deviceID, Sender: chatJID, Content: content, Timestamp: now}); err != nil {
			t.Fatalf("store message: %v", err)
		}
	}
	mustStore(jidKey, "m1", "under jid")
	mustStore(jidKey, "shared", "jid copy")
	mustStore(slotKey, "shared", "slot copy") // conflicting primary key under the target
	if err := repo.StoreMessageEdit(&domainChatStorage.MessageEdit{OriginalMessageID: "m1", EditEventID: "e1", ChatJID: chatJID, DeviceID: jidKey, NewContent: "edited", EditedAt: now}); err != nil {
		t.Fatalf("store edit: %v", err)
	}
	if err := repo.StoreReaction(&domainChatStorage.Reaction{MessageID: "m1", ChatJID: chatJID, DeviceID: jidKey, ReactorJID: chatJID, Emoji: "👍", Timestamp: now}); err != nil {
		t.Fatalf("store reaction: %v", err)
	}

	if err := repo.RekeyDeviceData(jidKey, slotKey); err != nil {
		t.Fatalf("RekeyDeviceData: %v", err)
	}

	if _, err := repo.GetMessageByIDAndDevice(slotKey, "m1"); err != nil {
		t.Fatalf("expected m1 under slot key: %v", err)
	}
	if msg, err := repo.GetMessageByIDAndDevice(slotKey, "shared"); err != nil || msg.Content != "slot copy" {
		t.Fatalf("expected existing target row to win, got %+v err=%v", msg, err)
	}
	if chat, err := repo.GetChatByDevice(slotKey, chatJID); err != nil || chat == nil {
		t.Fatalf("expected chat under slot key, got %+v err=%v", chat, err)
	}
	if edits, err := repo.GetMessageEdits("m1", slotKey); err != nil || len(edits) != 1 {
		t.Fatalf("expected 1 edit under slot key, got %d err=%v", len(edits), err)
	}
	var reactionCount int
	if err := repo.db.QueryRow("SELECT COUNT(*) FROM message_reactions WHERE device_id = ? AND message_id = ?", slotKey, "m1").Scan(&reactionCount); err != nil || reactionCount != 1 {
		t.Fatalf("expected 1 reaction under slot key, got %d err=%v", reactionCount, err)
	}

	var leftovers int
	for _, table := range []string{"chats", "messages", "message_edits", "message_reactions"} {
		var n int
		if err := repo.db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE device_id = ?", jidKey).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		leftovers += n
	}
	if leftovers != 0 {
		t.Fatalf("expected no rows left under %s, found %d", jidKey, leftovers)
	}

	// Moving back (re-pair) and no-op inputs must not error.
	if err := repo.RekeyDeviceData(slotKey, jidKey); err != nil {
		t.Fatalf("rekey back: %v", err)
	}
	if err := repo.RekeyDeviceData("", jidKey); err != nil {
		t.Fatalf("blank key should be a no-op: %v", err)
	}
	if err := repo.RekeyDeviceData(jidKey, jidKey); err != nil {
		t.Fatalf("same key should be a no-op: %v", err)
	}
}
