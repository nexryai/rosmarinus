package mongostore

import (
	"testing"
	"time"

	"github.com/nexryai/rosmarinus/internal/domain/instances"
)

func TestNormalizeInstanceHost(t *testing.T) {
	host, err := normalizeInstanceHost("BÜCHER.Example.")
	if err != nil {
		t.Fatalf("normalizeInstanceHost returned error: %v", err)
	}
	if host != "xn--bcher-kva.example" {
		t.Fatalf("host = %q", host)
	}
	if instanceID(host) != instanceID("xn--bcher-kva.example") {
		t.Fatal("instance ID is not deterministic")
	}
	for _, invalid := range []string{"", "bad host", "example.test:443", "https://example.test"} {
		if _, err := normalizeInstanceHost(invalid); err == nil {
			t.Fatalf("invalid host %q was accepted", invalid)
		}
	}
}

func TestToInstancePreservesOperationalState(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	open := true
	doc := instanceDocument{
		ID: "instance-id", Host: "remote.example", UsersCount: 2, NotesCount: 3,
		FollowingCount: 4, FollowersCount: 5, LatestRequestReceivedAt: &now,
		LatestRequestSentAt: &now, LatestStatus: 503, IsNotResponding: true,
		NotRespondingSince: &now, SuspensionState: instances.SuspensionAutoNotResponding,
		SoftwareName: "misskey", SoftwareVersion: "2026.8.0", OpenRegistrations: &open,
		Name: "Remote", Description: "description", MaintainerName: "Alice",
		MaintainerEmail: "admin@example.test", IconURL: "https://remote.example/icon.png",
		FaviconURL: "https://remote.example/favicon.ico", ThemeColor: "#123456",
		FirstRetrievedAt: now, InfoUpdatedAt: &now, UpdatedAt: now,
	}
	got := toInstance(doc)
	if got.ID != doc.ID || got.Host != doc.Host || got.FollowingCount != 4 || got.FollowersCount != 5 || got.LatestStatus != 503 || !got.IsNotResponding || got.SuspensionState != instances.SuspensionAutoNotResponding || got.SoftwareName != "misskey" || got.OpenRegistrations == nil || !*got.OpenRegistrations {
		t.Fatalf("unexpected instance projection: %+v", got)
	}
}
