package account

import (
	"testing"
	"time"
)

func TestAccountIsActive(t *testing.T) {
	active := &Account{Status: StatusActive}
	if !active.IsActive() {
		t.Fatal("active account should be active")
	}
	now := time.Now()
	for _, account := range []*Account{
		nil,
		{Status: StatusSuspended},
		{Status: StatusDeleted},
		{Status: StatusActive, DeletedAt: &now},
	} {
		if account.IsActive() {
			t.Fatalf("account should not be active: %+v", account)
		}
	}
}
