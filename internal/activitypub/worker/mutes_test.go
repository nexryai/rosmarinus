package worker

import (
	"context"
	"testing"
	"time"

	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/connector"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/mutes"
)

type fakeMuteRepo struct {
	mute *mutes.Mute
}

func (r *fakeMuteRepo) FindActive(_ context.Context, muterID, muteeID string, now time.Time) (*mutes.Mute, error) {
	if r.mute != nil && r.mute.MuterID == muterID && r.mute.MuteeID == muteeID && r.mute.Active(now) {
		return r.mute, nil
	}
	return nil, nil
}

func (r *fakeMuteRepo) ListActiveMuteeIDs(_ context.Context, muterID string, now time.Time) ([]string, error) {
	if r.mute != nil && r.mute.MuterID == muterID && r.mute.Active(now) {
		return []string{r.mute.MuteeID}, nil
	}
	return nil, nil
}

func (r *fakeMuteRepo) Upsert(_ context.Context, mute mutes.Mute) (*mutes.Mute, error) {
	mute.ID = "mute-1"
	r.mute = &mute
	return r.mute, nil
}

func (r *fakeMuteRepo) Delete(_ context.Context, muterID, muteeID string) error {
	if r.mute != nil && r.mute.MuterID == muterID && r.mute.MuteeID == muteeID {
		r.mute = nil
	}
	return nil
}

func TestCreateAndDeleteMuteRemainLocal(t *testing.T) {
	local := &actors.Actor{ID: "local-1", URI: "https://local.test/users/alice"}
	remoteHost := "remote.test"
	remote := &actors.Actor{ID: "remote-1", URI: "https://remote.test/users/bob", Host: &remoteHost}
	repository := &fakeMuteRepo{}
	handler := New(config.Config{}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)
	handler.SetMuteRepository(repository)
	expiresAt := time.Now().UTC().Add(time.Hour)

	created, err := handler.CreateMute(context.Background(), connector.MuteCreateCommand{ActorID: local.ID, Target: remote.URI, ExpiresAt: &expiresAt})
	if err != nil {
		t.Fatal(err)
	}
	if created.MuteID != "mute-1" || created.MuteeID != remote.ID || repository.mute == nil {
		t.Fatalf("created mute = %#v stored=%#v", created, repository.mute)
	}
	if _, err := handler.DeleteMute(context.Background(), connector.MuteDeleteCommand{ActorID: local.ID, Target: remote.URI}); err != nil {
		t.Fatal(err)
	}
	if repository.mute != nil {
		t.Fatalf("mute was not deleted: %#v", repository.mute)
	}
}
