package mutes

import (
	"testing"
	"time"
)

func TestMuteActive(t *testing.T) {
	now := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	indefinite := Mute{}
	future := now.Add(time.Hour)
	past := now.Add(-time.Second)
	if !indefinite.Active(now) || !(Mute{ExpiresAt: &future}).Active(now) || (Mute{ExpiresAt: &past}).Active(now) {
		t.Fatal("mute activity did not respect its expiration")
	}
}
