package mongostore

import (
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/nexryai/rosmarinus/internal/domain/antennas"
)

func TestAntennaDocumentPreservesMatchingContract(t *testing.T) {
	now := time.Date(2026, time.September, 15, 0, 0, 0, 0, time.UTC)
	doc := fromAntenna(antennas.Antenna{
		ID: "antenna-1", OwnerAccountID: "account-1", OwnerActorID: "actor-1", Name: "Rosemary",
		Source: antennas.SourceUsers, Users: []string{"@alice@example.test"}, Keywords: [][]string{{"go", "activitypub"}},
		ExcludeKeywords: [][]string{{"spam"}}, CaseSensitive: true, LocalOnly: true, ExcludeBots: true,
		WithReplies: true, WithFile: true, CreatedAt: now, UpdatedAt: now,
	})
	result := toAntenna(doc)
	if result.ID != "antenna-1" || result.Source != antennas.SourceUsers || len(result.Keywords) != 1 || !result.WithFile || !result.ExcludeBots {
		t.Fatalf("antenna = %+v", result)
	}
}

func TestAntennaKeywordFilterUsesORGroupsOfANDWords(t *testing.T) {
	filter := antennaKeywordFilter([][]string{{"Go", "ActivityPub"}, {"Rosemary"}}, false)
	encoded, err := bson.MarshalExtJSON(filter, false, false)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if !strings.Contains(text, `"$or"`) || !strings.Contains(text, `"$and"`) || !strings.Contains(text, `"options":"i"`) || !strings.Contains(text, `"contentWarning"`) {
		t.Fatalf("filter = %s", text)
	}
	caseSensitive, err := bson.MarshalExtJSON(antennaKeywordFilter([][]string{{"Go"}}, true), false, false)
	if err != nil || strings.Contains(string(caseSensitive), `"options":"i"`) {
		t.Fatalf("case-sensitive filter = %s err=%v", caseSensitive, err)
	}
	excluded, err := bson.MarshalExtJSON(antennaExcludeKeywordFilter([][]string{{"spam"}}, false), false, false)
	if err != nil || !strings.Contains(string(excluded), `"$nor"`) {
		t.Fatalf("exclude filter = %s err=%v", excluded, err)
	}
}
