package worker

import (
	"context"
	"testing"

	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/connector"
	"github.com/nexryai/rosmarinus/internal/domain/antennas"
)

type fakeAntennaRepository struct {
	count int64
	item  *antennas.Antenna
}

func (r *fakeAntennaRepository) Count(context.Context, string, string) (int64, error) {
	return r.count, nil
}

func (r *fakeAntennaRepository) Create(_ context.Context, antenna antennas.Antenna) (*antennas.Antenna, error) {
	antenna.ID = "antenna-1"
	r.item = &antenna
	return r.item, nil
}

func (r *fakeAntennaRepository) Update(_ context.Context, antenna antennas.Antenna) (*antennas.Antenna, error) {
	r.item = &antenna
	return r.item, nil
}

func (r *fakeAntennaRepository) Delete(_ context.Context, accountID, actorID, antennaID string) (bool, error) {
	if r.item == nil || r.item.OwnerAccountID != accountID || r.item.OwnerActorID != actorID || r.item.ID != antennaID {
		return false, nil
	}
	r.item = nil
	return true, nil
}

func TestAntennaCommandsPreserveAccountAndActorOwnership(t *testing.T) {
	repository := &fakeAntennaRepository{}
	handler := New(config.Config{}, nil, &fakeRepo{}, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, nil)
	handler.SetAntennaRepository(repository)
	input := connector.AntennaInput{Name: "Go", Source: "all", Keywords: [][]string{{"Go", "ActivityPub"}}, WithReplies: true}
	created, err := handler.CreateAntenna(context.Background(), "account-1", connector.AntennaCreateCommand{ActorID: "actor-1", Input: input})
	if err != nil {
		t.Fatal(err)
	}
	if created.AntennaID != "antenna-1" || repository.item == nil || repository.item.OwnerAccountID != "account-1" || repository.item.OwnerActorID != "actor-1" {
		t.Fatalf("created=%+v stored=%+v", created, repository.item)
	}
	if _, err := handler.DeleteAntenna(context.Background(), "account-2", connector.AntennaDeleteCommand{ActorID: "actor-1", AntennaID: created.AntennaID}); err == nil {
		t.Fatal("another account deleted the antenna")
	}
}

func TestCreateAntennaEnforcesActorLimit(t *testing.T) {
	repository := &fakeAntennaRepository{count: antennas.MaxPerActor}
	handler := New(config.Config{}, nil, &fakeRepo{}, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, nil)
	handler.SetAntennaRepository(repository)
	_, err := handler.CreateAntenna(context.Background(), "account-1", connector.AntennaCreateCommand{ActorID: "actor-1", Input: connector.AntennaInput{Name: "Go", Source: "all", Keywords: [][]string{{"Go"}}}})
	if err == nil || repository.item != nil {
		t.Fatalf("limit error=%v stored=%+v", err, repository.item)
	}
}
