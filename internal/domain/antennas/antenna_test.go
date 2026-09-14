package antennas

import "testing"

func TestNormalizeRequiresUsefulCriteria(t *testing.T) {
	_, err := Normalize(Antenna{OwnerAccountID: "account-1", OwnerActorID: "actor-1", Name: "news", Source: SourceAll})
	if err == nil {
		t.Fatal("empty antenna criteria were accepted")
	}
	antenna, err := Normalize(Antenna{
		OwnerAccountID: " account-1 ", OwnerActorID: " actor-1 ", Name: " News ", Source: SourceUsers,
		Users: []string{" @alice@example.test ", "@ALICE@example.test"}, Keywords: [][]string{{" rosemary ", ""}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if antenna.Name != "News" || len(antenna.Users) != 1 || len(antenna.Keywords) != 1 || len(antenna.Keywords[0]) != 1 {
		t.Fatalf("normalized antenna = %+v", antenna)
	}
}
