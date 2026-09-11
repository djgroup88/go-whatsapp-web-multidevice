package chatstorage

import (
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

func TestReachoutTimelockRoundTrip(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	if err := repo.SaveDeviceRecord(&domainChatStorage.DeviceRecord{DeviceID: "dev-1"}); err != nil {
		t.Fatalf("SaveDeviceRecord() error = %v", err)
	}

	// A device that has never been restricted reads as zero, not as an error.
	if _, endsAt, err := repo.GetReachoutTimelock("dev-1"); err != nil || endsAt != 0 {
		t.Fatalf("fresh device: endsAt = %d, err = %v", endsAt, err)
	}
	// So does an unknown device.
	if _, endsAt, err := repo.GetReachoutTimelock("does-not-exist"); err != nil || endsAt != 0 {
		t.Fatalf("unknown device: endsAt = %d, err = %v", endsAt, err)
	}

	want := time.Now().Add(6 * time.Hour).Unix()
	if err := repo.SaveReachoutTimelock("dev-1", "reachout", want); err != nil {
		t.Fatalf("SaveReachoutTimelock() error = %v", err)
	}

	gotType, gotEndsAt, err := repo.GetReachoutTimelock("dev-1")
	if err != nil {
		t.Fatalf("GetReachoutTimelock() error = %v", err)
	}
	if gotType != "reachout" || gotEndsAt != want {
		t.Fatalf("got (%q, %d), want (%q, %d)", gotType, gotEndsAt, "reachout", want)
	}

	// Clearing is a zero write, not a delete of the device.
	if err := repo.SaveReachoutTimelock("dev-1", "", 0); err != nil {
		t.Fatalf("clear error = %v", err)
	}
	if _, gotEndsAt, err = repo.GetReachoutTimelock("dev-1"); err != nil || gotEndsAt != 0 {
		t.Fatalf("after clear: endsAt = %d, err = %v", gotEndsAt, err)
	}
	if rec, err := repo.GetDeviceRecord("dev-1"); err != nil || rec == nil {
		t.Fatalf("device record must survive clearing the timelock: %v", err)
	}

	// The lock must not leak to another device.
	if err := repo.SaveDeviceRecord(&domainChatStorage.DeviceRecord{DeviceID: "dev-2"}); err != nil {
		t.Fatalf("SaveDeviceRecord() error = %v", err)
	}
	if err := repo.SaveReachoutTimelock("dev-1", "reachout", want); err != nil {
		t.Fatalf("SaveReachoutTimelock() error = %v", err)
	}
	if _, endsAt, err := repo.GetReachoutTimelock("dev-2"); err != nil || endsAt != 0 {
		t.Fatalf("dev-2 must be unaffected: endsAt = %d, err = %v", endsAt, err)
	}
}
