package federation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/nexryai/rosmarinus/internal/account"
	apworker "github.com/nexryai/rosmarinus/internal/activitypub/worker"
	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/connector"
	"github.com/nexryai/rosmarinus/internal/domain/activities"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/blocks"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	"github.com/nexryai/rosmarinus/internal/domain/instances"
	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
	domainpolls "github.com/nexryai/rosmarinus/internal/domain/polls"
	"github.com/nexryai/rosmarinus/internal/domain/reactions"
	"github.com/nexryai/rosmarinus/internal/realtime"
	mongostore "github.com/nexryai/rosmarinus/internal/store/mongo"
)

type misskeyFederationPhaseState struct {
	ctx                 context.Context
	worker              *apworker.Handler
	cfg                 config.Config
	db                  *mongo.Database
	actorRepo           actors.Repository
	noteRepo            domainnotes.Repository
	followRepo          follows.Repository
	blockRepo           blocks.Repository
	reactionRepo        reactions.Repository
	pollRepo            domainpolls.Repository
	mediaRepo           *mongostore.MediaRepository
	instanceRepo        instances.Repository
	activityReceiptRepo activities.ReceiptRepository
	localActor          *actors.Actor
	remoteActor         *actors.Actor
	remoteActorURI      string
	remoteNote          *domainnotes.Note
	remoteMisskeyNoteID string
	misskey             *misskeyClient
	admin               misskeyAccount
	misskeyB            *misskeyClient
	adminB              misskeyAccount
	directRecipient     misskeyAccount
	directRecipientURI  string
	relayOnMisskeyID    string
	avatarPNG           []byte
	ownedActor          *actors.Actor
	ownedMisskeyID      string
}

func runLatestMisskeyFederationPhases13To24(t *testing.T, state misskeyFederationPhaseState) {
	ctx := state.ctx
	worker := state.worker
	cfg := state.cfg
	db := state.db
	actorRepo := state.actorRepo
	noteRepo := state.noteRepo
	followRepo := state.followRepo
	blockRepo := state.blockRepo
	reactionRepo := state.reactionRepo
	pollRepo := state.pollRepo
	mediaRepo := state.mediaRepo
	instanceRepo := state.instanceRepo
	activityReceiptRepo := state.activityReceiptRepo
	localActor := state.localActor
	remoteActor := state.remoteActor
	remoteActorURI := state.remoteActorURI
	remoteNote := state.remoteNote
	misskey := state.misskey
	admin := state.admin
	misskeyB := state.misskeyB
	adminB := state.adminB
	directRecipient := state.directRecipient
	directRecipientURI := state.directRecipientURI
	avatarPNG := state.avatarPNG
	ownedActor := state.ownedActor
	ownedMisskeyID := state.ownedMisskeyID
	relayOnMisskey := struct{ ID string }{ID: state.relayOnMisskeyID}
	created := struct{ CreatedNote struct{ ID string } }{}
	created.CreatedNote.ID = state.remoteMisskeyNoteID

	// Phase 13: block the Misskey Actor from Rosmarinus, verify Misskey removes
	// its local Follow and exposes the blocked relationship, then undo the Block.
	// Have Misskey request another Follow, reject and block it in one operation,
	// verify both effects, then undo and prove the Actor can follow again.
	createdBlock, err := worker.CreateBlock(ctx, connector.BlockCreateCommand{
		ActorID: localActor.ID,
		Target:  remoteActorURI,
	})
	if err != nil {
		t.Fatalf("create outbound Block: %v", err)
	}
	if createdBlock.BlockeeID != remoteActor.ID || createdBlock.URI == "" {
		t.Fatalf("unexpected created Block: %+v", createdBlock)
	}
	waitFor(t, ctx, "Block applied by Misskey", func() bool {
		var shown struct {
			IsFollowing bool `json:"isFollowing"`
			IsBlocked   bool `json:"isBlocked"`
		}
		misskey.call(ctx, "users/show", map[string]any{
			"i": admin.Token, "userId": relayOnMisskey.ID,
		}, &shown)
		return !shown.IsFollowing && shown.IsBlocked
	})
	deletedBlock, err := worker.DeleteBlock(ctx, connector.BlockDeleteCommand{
		ActorID: localActor.ID,
		Target:  remoteActorURI,
	})
	if err != nil {
		t.Fatalf("delete outbound Block: %v", err)
	}
	if deletedBlock.BlockID != createdBlock.BlockID || deletedBlock.URI != createdBlock.URI+"/undo" {
		t.Fatalf("unexpected deleted Block: %+v", deletedBlock)
	}
	waitFor(t, ctx, "Undo(Block) applied by Misskey", func() bool {
		var shown struct {
			IsBlocked bool `json:"isBlocked"`
		}
		misskey.call(ctx, "users/show", map[string]any{
			"i": admin.Token, "userId": relayOnMisskey.ID,
		}, &shown)
		return !shown.IsBlocked
	})
	misskey.call(ctx, "following/create", map[string]any{
		"i": admin.Token, "userId": relayOnMisskey.ID,
	}, nil)
	waitFor(t, ctx, "post-unblock Follow stored as pending", func() bool {
		inbound, findErr := followRepo.Find(ctx, remoteActor.ID, localActor.ID)
		return findErr == nil && inbound != nil && inbound.Status == follows.StatusPending
	})
	rejectedBlock, err := worker.RejectAndBlockFollow(ctx, remoteActor.ID, localActor.ID)
	if err != nil {
		t.Fatalf("reject and block inbound Follow: %v", err)
	}
	if rejectedBlock.BlockeeID != remoteActor.ID || rejectedBlock.URI == "" {
		t.Fatalf("unexpected reject-and-block result: %+v", rejectedBlock)
	}
	waitFor(t, ctx, "Reject and Block applied by Misskey", func() bool {
		var shown struct {
			IsFollowing bool `json:"isFollowing"`
			IsBlocked   bool `json:"isBlocked"`
		}
		misskey.call(ctx, "users/show", map[string]any{
			"i": admin.Token, "userId": relayOnMisskey.ID,
		}, &shown)
		return !shown.IsFollowing && shown.IsBlocked
	})
	if pending, findErr := followRepo.Find(ctx, remoteActor.ID, localActor.ID); findErr != nil || pending != nil {
		t.Fatalf("rejected Follow remains: follow=%+v err=%v", pending, findErr)
	}
	if _, err := worker.DeleteBlock(ctx, connector.BlockDeleteCommand{ActorID: localActor.ID, Target: remoteActorURI}); err != nil {
		t.Fatalf("delete reject-and-block Block: %v", err)
	}
	waitFor(t, ctx, "reject-and-block Undo(Block) applied by Misskey", func() bool {
		var shown struct {
			IsBlocked bool `json:"isBlocked"`
		}
		misskey.call(ctx, "users/show", map[string]any{
			"i": admin.Token, "userId": relayOnMisskey.ID,
		}, &shown)
		return !shown.IsBlocked
	})
	misskey.call(ctx, "following/create", map[string]any{
		"i": admin.Token, "userId": relayOnMisskey.ID,
	}, nil)
	waitFor(t, ctx, "post-reject-and-block Follow stored as pending", func() bool {
		inbound, findErr := followRepo.Find(ctx, remoteActor.ID, localActor.ID)
		return findErr == nil && inbound != nil && inbound.Status == follows.StatusPending
	})
	if _, err := worker.ApproveFollow(ctx, remoteActor.ID, localActor.ID); err != nil {
		t.Fatalf("approve post-reject-and-block Follow: %v", err)
	}
	waitFor(t, ctx, "post-reject-and-block Follow accepted by Misskey", func() bool {
		var shown struct {
			IsFollowing bool `json:"isFollowing"`
		}
		misskey.call(ctx, "users/show", map[string]any{
			"i": admin.Token, "userId": relayOnMisskey.ID,
		}, &shown)
		return shown.IsFollowing
	})
	if stored, findErr := blockRepo.Find(ctx, localActor.ID, remoteActor.ID); findErr != nil || stored != nil {
		t.Fatalf("outbound Block remains after undo: block=%+v err=%v", stored, findErr)
	}

	// Phase 14: renote the stored Misskey Note from Rosmarinus, verify Misskey
	// accepts the outbound Announce, then delete it and verify Undo(Announce).
	createdLocalRenote, err := worker.CreatePost(ctx, connector.PostCreateCommand{
		ActorID: localActor.ID, RenoteID: remoteNote.ID,
		Visibility: string(domainnotes.VisibilityPublic),
	})
	if err != nil {
		t.Fatalf("create local Rosmarinus renote: %v", err)
	}
	assertObjectIDString(t, "local renote", createdLocalRenote.NoteID)
	var localAnnounce map[string]any
	misskey.get(ctx, createdLocalRenote.URI+"/activity", &localAnnounce)
	if localAnnounce["type"] != "Announce" || localAnnounce["actor"] != localActor.URI || localAnnounce["object"] != remoteNote.URI {
		t.Fatalf("unexpected outbound Announce activity: %#v", localAnnounce)
	}
	localAnnounceURI := createdLocalRenote.URI + "/activity"
	waitFor(t, ctx, "Announce stored by Misskey", func() bool {
		var notes []struct {
			URI    string `json:"uri"`
			Renote *struct {
				ID string `json:"id"`
			} `json:"renote"`
		}
		misskey.call(ctx, "users/notes", map[string]any{
			"i": admin.Token, "userId": relayOnMisskey.ID, "limit": 20,
		}, &notes)
		for _, note := range notes {
			if note.URI == localAnnounceURI && note.Renote != nil && note.Renote.ID == created.CreatedNote.ID {
				return true
			}
		}
		return false
	})
	if _, err := worker.DeletePost(ctx, connector.PostDeleteCommand{ActorID: localActor.ID, NoteID: createdLocalRenote.NoteID}); err != nil {
		t.Fatalf("delete local Rosmarinus renote: %v", err)
	}
	waitFor(t, ctx, "Undo(Announce) applied by Misskey", func() bool {
		var notes []struct {
			URI string `json:"uri"`
		}
		misskey.call(ctx, "users/notes", map[string]any{
			"i": admin.Token, "userId": relayOnMisskey.ID, "limit": 20,
		}, &notes)
		for _, note := range notes {
			if note.URI == localAnnounceURI {
				return false
			}
		}
		return true
	})

	// Phase 15: publish simple MFM with an Actor-owned local image and advanced
	// MFM from Rosmarinus, verify the first Note keeps safe HTML and attachment
	// metadata, the second retains its source, and current Misskey stores both
	// delivered Create(Note)s.
	const localNoteText = "Hello from Rosmarinus federation delivery :party:"
	const misskeyLocalNoteText = "Hello from Rosmarinus federation delivery \u200B:party:\u200B"
	localMedia, err := mediaRepo.CreateLocal(ctx, "federation-fixture", localActor.ID, "salvia.png", cfg.PublicURL+"/media", "image/png", int64(len(avatarPNG)), "federation-fixture", 1, 1, bytes.NewReader(avatarPNG))
	if err != nil || localMedia == nil {
		t.Fatalf("store local Salvia upload: media=%+v err=%v", localMedia, err)
	}
	assertObjectIDString(t, "local media", localMedia.ID)
	status, localMediaType, localMediaBody := misskey.getRaw(ctx, localMedia.PublicURL)
	if status != http.StatusOK || localMediaType != "image/png" || !bytes.Equal(localMediaBody, avatarPNG) {
		t.Fatalf("serve local Salvia upload: status=%d content_type=%q bytes=%d", status, localMediaType, len(localMediaBody))
	}
	worker.SetMediaRepository(mediaRepo, nil)
	createdLocal, err := worker.CreatePost(ctx, connector.PostCreateCommand{
		ActorID:    localActor.ID,
		Text:       localNoteText,
		EmojiNames: []string{"party"},
		MediaIDs:   []string{localMedia.ID},
		Visibility: string(domainnotes.VisibilityPublic),
		Poll:       &connector.PollCreateCommand{Choices: []string{"cats", "dogs"}},
	})
	if err != nil {
		t.Fatalf("create local Rosmarinus post: %v", err)
	}
	assertObjectIDString(t, "local Note", createdLocal.NoteID)
	localNoteID := createdLocal.NoteID
	t.Logf("Rosmarinus local note created note_id=%s uri=%s", createdLocal.NoteID, createdLocal.URI)
	var publicActivity map[string]any
	misskey.get(ctx, createdLocal.URI+"/activity", &publicActivity)
	if publicActivity["type"] != "Create" || publicActivity["actor"] != localActor.URI {
		t.Fatalf("unexpected public Create activity: %#v", publicActivity)
	}
	publicObject, _ := publicActivity["object"].(map[string]any)
	if tags, _ := publicObject["tag"].([]any); len(tags) == 0 {
		t.Fatalf("public Create activity omitted local Emoji tag: %#v", publicActivity)
	}
	publicContent, _ := publicObject["content"].(string)
	if !strings.Contains(publicContent, "\u200B:party:\u200B") || publicObject["_misskey_content"] != nil || publicObject["source"] != nil {
		t.Fatalf("simple MFM was not rendered with current Misskey semantics: %#v", publicObject)
	}
	var misskeyLocalNoteID, storedLocalNoteText string
	var storedLocalFiles int
	waitFor(t, ctx, "Create(Note) stored by Misskey", func() bool {
		var notes []struct {
			ID    string `json:"id"`
			Text  string `json:"text"`
			URI   string `json:"uri"`
			Files []struct {
				Type string `json:"type"`
			} `json:"files"`
		}
		misskey.call(ctx, "users/notes", map[string]any{
			"i":      admin.Token,
			"userId": relayOnMisskey.ID,
			"limit":  10,
		}, &notes)
		for _, note := range notes {
			if note.URI == createdLocal.URI {
				misskeyLocalNoteID = note.ID
				storedLocalNoteText = note.Text
				storedLocalFiles = len(note.Files)
				return len(note.Files) == 1
			}
		}
		return false
	})
	if storedLocalNoteText != misskeyLocalNoteText {
		t.Fatalf("Misskey stored simple MFM text %q, want %q", storedLocalNoteText, misskeyLocalNoteText)
	}
	if storedLocalFiles != 1 {
		t.Fatalf("Misskey stored %d local attachment files, want 1", storedLocalFiles)
	}
	var shownOnMisskeyB struct {
		Type   string `json:"type"`
		Object struct {
			ID  string `json:"id"`
			URI string `json:"uri"`
		} `json:"object"`
	}
	misskeyB.call(ctx, "ap/show", map[string]any{"i": adminB.Token, "uri": createdLocal.URI}, &shownOnMisskeyB)
	if shownOnMisskeyB.Type != "Note" || shownOnMisskeyB.Object.ID == "" || shownOnMisskeyB.Object.URI != createdLocal.URI {
		t.Fatalf("second Misskey did not resolve local Note: %+v", shownOnMisskeyB)
	}
	var replyFromMisskeyB struct {
		CreatedNote struct {
			ID string `json:"id"`
		} `json:"createdNote"`
	}
	misskeyB.call(ctx, "notes/create", map[string]any{
		"i": adminB.Token, "text": "Concerned remote reply", "replyId": shownOnMisskeyB.Object.ID,
	}, &replyFromMisskeyB)
	if replyFromMisskeyB.CreatedNote.ID == "" {
		t.Fatalf("second Misskey reply creation returned no note id")
	}
	misskeyBActorURI := "https://b.test/users/" + adminB.ID
	waitFor(t, ctx, "non-follower reply stored by Rosmarinus", func() bool {
		authors, listErr := noteRepo.ListActiveReferenceAuthorURIsPage(ctx, localNoteID, "", 100)
		if listErr != nil {
			return false
		}
		for _, uri := range authors {
			if uri == misskeyBActorURI {
				return true
			}
		}
		return false
	})

	const advancedNoteText = "**Hello from Rosmarinus** $[ruby 漢字 かんじ]"
	advancedLocal, err := worker.CreatePost(ctx, connector.PostCreateCommand{
		ActorID: localActor.ID, Text: advancedNoteText,
		Visibility: string(domainnotes.VisibilityPublic),
	})
	if err != nil {
		t.Fatalf("create advanced MFM post: %v", err)
	}
	var advancedActivity map[string]any
	misskey.get(ctx, advancedLocal.URI+"/activity", &advancedActivity)
	advancedObject, _ := advancedActivity["object"].(map[string]any)
	advancedContent, _ := advancedObject["content"].(string)
	advancedSource, _ := advancedObject["source"].(map[string]any)
	if !strings.Contains(advancedContent, "<b>Hello from Rosmarinus</b>") ||
		!strings.Contains(advancedContent, "<ruby>漢字") ||
		advancedObject["_misskey_content"] != advancedNoteText ||
		advancedSource["content"] != advancedNoteText || advancedSource["mediaType"] != "text/x.misskeymarkdown" {
		t.Fatalf("advanced MFM compatibility fields are incomplete: %#v", advancedObject)
	}
	var storedAdvancedNoteText string
	waitFor(t, ctx, "advanced MFM Create(Note) stored by Misskey", func() bool {
		var notes []struct {
			Text string `json:"text"`
			URI  string `json:"uri"`
		}
		misskey.call(ctx, "users/notes", map[string]any{
			"i": admin.Token, "userId": relayOnMisskey.ID, "limit": 10,
		}, &notes)
		for _, note := range notes {
			if note.URI == advancedLocal.URI {
				storedAdvancedNoteText = note.Text
				return true
			}
		}
		return false
	})
	if storedAdvancedNoteText != advancedNoteText {
		t.Fatalf("Misskey stored advanced MFM text %q, want %q", storedAdvancedNoteText, advancedNoteText)
	}

	// Phase 16: vote on the delivered Rosmarinus Question from Misskey and
	// verify Rosmarinus consumes the reply Note as a poll vote.
	misskey.call(ctx, "notes/polls/vote", map[string]any{
		"i": admin.Token, "noteId": misskeyLocalNoteID, "choice": 1,
	}, nil)
	waitFor(t, ctx, "Misskey poll vote stored by Rosmarinus", func() bool {
		poll, findErr := pollRepo.FindByNoteID(ctx, localNoteID)
		return findErr == nil && poll != nil && len(poll.Votes) == 2 && poll.Votes[1] == 1
	})

	// Phase 17: react to the delivered Rosmarinus Note from Misskey, verify the
	// stored Reaction has an ObjectID-string ID, and dereference its Like activity.
	misskey.call(ctx, "notes/reactions/create", map[string]any{
		"i":        admin.Token,
		"noteId":   misskeyLocalNoteID,
		"reaction": "👍",
	}, nil)
	var storedReaction *reactions.Reaction
	waitFor(t, ctx, "Misskey reaction stored by Rosmarinus", func() bool {
		var findErr error
		storedReaction, findErr = reactionRepo.Find(ctx, localNoteID, remoteActor.ID)
		return findErr == nil && storedReaction != nil && storedReaction.Reaction == "👍"
	})
	assertObjectIDString(t, "Reaction", storedReaction.ID)
	likeActivityURI := cfg.PublicURL + "/likes/" + url.PathEscape(storedReaction.ID)
	var likeActivity map[string]any
	misskey.get(ctx, likeActivityURI, &likeActivity)
	if likeActivity["type"] != "Like" || likeActivity["actor"] != remoteActorURI || likeActivity["object"] != createdLocal.URI || likeActivity["_misskey_reaction"] != "👍" {
		t.Fatalf("unexpected Like activity: %#v", likeActivity)
	}

	// Phase 18: soft-delete the local Note, verify Rosmarinus removes its Poll,
	// votes, and reactions, and verify current Misskey accepts the identified
	// Delete(Tombstone) activity. The second Misskey is not a follower, so its
	// federated copy disappearing also verifies delivery to a remote reply
	// author that is concerned with the deleted Note.
	deletedLocal, err := worker.DeletePost(ctx, connector.PostDeleteCommand{ActorID: localActor.ID, NoteID: localNoteID})
	if err != nil {
		t.Fatalf("delete local Rosmarinus post: %v", err)
	}
	if deletedLocal.NoteID != localNoteID || deletedLocal.URI != createdLocal.URI {
		t.Fatalf("unexpected deleted local post: %+v", deletedLocal)
	}
	waitFor(t, ctx, "Delete(Tombstone) applied by Misskey", func() bool {
		var notes []struct {
			URI string `json:"uri"`
		}
		status := misskey.callStatus(ctx, "users/notes", map[string]any{
			"i": admin.Token, "userId": relayOnMisskey.ID, "limit": 10,
		}, &notes)
		// Misskey removes the poll before its Note, so packing users/notes can
		// transiently fail while the asynchronous delete converges.
		if status >= http.StatusInternalServerError {
			return false
		}
		if status < http.StatusOK || status >= http.StatusMultipleChoices {
			t.Fatalf("Misskey users/notes status=%d", status)
		}
		for _, note := range notes {
			if note.URI == createdLocal.URI {
				return false
			}
		}
		return true
	})
	waitFor(t, ctx, "Delete(Tombstone) delivered to non-follower reply author", func() bool {
		// Current Misskey reports NO_SUCH_NOTE as a client error rather than
		// mapping it to HTTP 404.
		return misskeyB.callStatus(ctx, "notes/show", map[string]any{
			"i": adminB.Token, "noteId": shownOnMisskeyB.Object.ID,
		}, nil) == http.StatusBadRequest
	})
	if poll, findErr := pollRepo.FindByNoteID(ctx, localNoteID); findErr != nil || poll != nil {
		t.Fatalf("deleted Note poll remains: poll=%+v err=%v", poll, findErr)
	}
	if reaction, findErr := reactionRepo.Find(ctx, localNoteID, remoteActor.ID); findErr != nil || reaction != nil {
		t.Fatalf("deleted Note reaction remains: reaction=%+v err=%v", reaction, findErr)
	}
	if votes, countErr := db.Collection("poll_votes").CountDocuments(ctx, map[string]any{"noteId": localNoteID}); countErr != nil || votes != 0 {
		t.Fatalf("deleted Note poll votes remain: count=%d err=%v", votes, countErr)
	}

	// Phase 19: let a non-following Misskey Actor publish a target Note, then
	// reply with specified visibility from Rosmarinus. Verify Rosmarinus resolves
	// and persists the reply target, keeps the reply non-dereferenceable, and
	// directly delivers it to the target author without an explicit mention.
	var specifiedReplyTarget struct {
		CreatedNote struct {
			ID string `json:"id"`
		} `json:"createdNote"`
	}
	misskey.call(ctx, "notes/create", map[string]any{
		"i": directRecipient.Token, "text": "Reply target for Rosmarinus",
	}, &specifiedReplyTarget)
	if specifiedReplyTarget.CreatedNote.ID == "" {
		t.Fatalf("specified reply target creation returned no note id")
	}
	specifiedReplyTargetURI := "https://a.test/notes/" + specifiedReplyTarget.CreatedNote.ID
	const specifiedNoteText = "Private hello from Rosmarinus"
	createdSpecified, err := worker.CreatePost(ctx, connector.PostCreateCommand{
		ActorID: localActor.ID, Text: specifiedNoteText,
		Visibility: string(domainnotes.VisibilitySpecified), InReplyToURI: specifiedReplyTargetURI,
	})
	if err != nil {
		t.Fatalf("create specified Rosmarinus post: %v", err)
	}
	t.Logf("Rosmarinus specified note created note_id=%s uri=%s recipient=%s", createdSpecified.NoteID, createdSpecified.URI, directRecipientURI)
	if status := misskey.getStatus(ctx, createdSpecified.URI+"/activity"); status != http.StatusNotFound {
		t.Fatalf("specified Create activity status = %d, want %d", status, http.StatusNotFound)
	}
	storedSpecified, err := noteRepo.FindByID(ctx, createdSpecified.NoteID)
	if err != nil || storedSpecified == nil || storedSpecified.InReplyToURI != specifiedReplyTargetURI || storedSpecified.ReplyID == "" {
		t.Fatalf("specified reply target was not resolved: note=%+v err=%v", storedSpecified, err)
	}
	waitFor(t, ctx, "specified Create(Note) stored for Misskey recipient", func() bool {
		var notes []struct {
			Text    string `json:"text"`
			URI     string `json:"uri"`
			ReplyID string `json:"replyId"`
		}
		misskey.call(ctx, "users/notes", map[string]any{
			"i":           directRecipient.Token,
			"userId":      relayOnMisskey.ID,
			"limit":       10,
			"withReplies": true,
		}, &notes)
		for _, note := range notes {
			if note.Text == specifiedNoteText && note.URI == createdSpecified.URI && note.ReplyID == specifiedReplyTarget.CreatedNote.ID {
				return true
			}
		}
		return false
	})

	// Phase 20: verify first contact, authenticated inbox traffic, successful
	// deliveries, relationship changes, and daily NodeInfo discovery converge on
	// one current-Misskey instance document that Salvia can read.
	type instanceSnapshot struct {
		Found, InfoUpdated, RequestReceived, RequestSent bool
		Error, Software, Suspension                      string
		Status                                           int
		NotResponding                                    bool
		Users, Following, Followers                      int64
	}
	var previousInstanceSnapshot *instanceSnapshot
	waitFor(t, ctx, "Misskey instance metadata and federation stats stored", func() bool {
		instance, findErr := instanceRepo.FindByHost(ctx, "a.test")
		snapshot := instanceSnapshot{}
		if findErr != nil {
			snapshot.Error = findErr.Error()
		}
		if instance != nil {
			snapshot.Found = true
			snapshot.Software = instance.SoftwareName
			snapshot.InfoUpdated = instance.InfoUpdatedAt != nil
			snapshot.RequestReceived = instance.LatestRequestReceivedAt != nil
			snapshot.RequestSent = instance.LatestRequestSentAt != nil
			snapshot.Status = instance.LatestStatus
			snapshot.NotResponding = instance.IsNotResponding
			snapshot.Suspension = instance.SuspensionState
			snapshot.Users = instance.UsersCount
			snapshot.Following = instance.FollowingCount
			snapshot.Followers = instance.FollowersCount
		}
		if previousInstanceSnapshot == nil || *previousInstanceSnapshot != snapshot {
			t.Logf("Misskey instance snapshot: %+v", snapshot)
			previousInstanceSnapshot = &snapshot
		}
		// Current Misskey derives NodeInfo user totals from an asynchronous chart,
		// which can still report zero immediately after fixture account creation.
		return findErr == nil && instance != nil && instance.SoftwareName == "misskey" &&
			instance.InfoUpdatedAt != nil && instance.LatestRequestReceivedAt != nil &&
			instance.LatestRequestSentAt != nil && instance.LatestStatus >= 200 && instance.LatestStatus < 300 &&
			!instance.IsNotResponding && instance.SuspensionState == "none" &&
			instance.FollowingCount == 2 && instance.FollowersCount == 0
	})

	// Phase 21: inspect a completed receipt from authenticated Misskey traffic
	// and verify the MongoDB claim rejects its Activity URI as a duplicate.
	var receipt struct {
		ActivityID string `bson:"activityId"`
		ActorURI   string `bson:"actorUri"`
		Status     string `bson:"status"`
	}
	if err := db.Collection("inbox_activity_receipts").FindOne(ctx, bson.M{
		"actorUri": remoteActorURI,
		"status":   "completed",
	}).Decode(&receipt); err != nil {
		t.Fatalf("find completed Misskey activity receipt: %v", err)
	}
	if receipt.ActivityID == "" || receipt.ActorURI != remoteActorURI {
		t.Fatalf("unexpected completed activity receipt: %+v", receipt)
	}
	if _, claimed, err := activityReceiptRepo.Claim(
		ctx,
		receipt.ActivityID,
		receipt.ActorURI,
		time.Now().UTC(),
		cfg.InboxQueue.Timeout+time.Minute,
		cfg.InboxActivityReceiptTTL,
	); err != nil || claimed {
		t.Fatalf("completed Misskey activity was claimable again: claimed=%t err=%v", claimed, err)
	}

	// Phase 22: suspend the owning Salvia account, verifying Rosmarinus hides its
	// Actor and current Misskey applies the uniquely identified signed
	// Delete(Actor). Reactivate the account and verify Rosmarinus restores the
	// Actor endpoint while emitting the matching Undo(Delete); current Misskey
	// retains its remote tombstone and may require a later explicit re-resolution.
	modified, err := worker.ApplyAccountLifecycle(ctx, "federation-account", account.StatusSuspended, false)
	if err != nil || modified != 1 {
		t.Fatalf("suspend account-owned Actor: modified=%d err=%v", modified, err)
	}
	if status := misskey.getStatus(ctx, ownedActor.URI); status != http.StatusNotFound {
		t.Fatalf("suspended Actor ActivityPub endpoint status=%d, want %d", status, http.StatusNotFound)
	}
	waitFor(t, ctx, "Misskey applies account-suspension Delete(Actor)", func() bool {
		status := misskey.callStatus(ctx, "users/show", map[string]any{
			"i": directRecipient.Token, "userId": ownedMisskeyID,
		}, nil)
		return status == http.StatusNotFound
	})
	modified, err = worker.ApplyAccountLifecycle(ctx, "federation-account", account.StatusActive, false)
	if err != nil || modified != 1 {
		t.Fatalf("resume account-owned Actor: modified=%d err=%v", modified, err)
	}
	if active, findErr := actorRepo.FindOwnedLocalByID(ctx, "federation-account", ownedActor.ID); findErr != nil || active == nil || active.IsSuspended {
		t.Fatalf("resumed account-owned Actor is not active: actor=%+v err=%v", active, findErr)
	}
	if status := misskey.getStatus(ctx, ownedActor.URI); status != http.StatusOK {
		t.Fatalf("resumed Actor ActivityPub endpoint status=%d, want %d", status, http.StatusOK)
	}

	// Phase 23: permanently delete the resumed account-owned local Actor,
	// verifying Rosmarinus tombstones it and current Misskey continues hiding the
	// remote Actor after receiving the distinct signed permanent Delete(Actor).
	deletedOwned, err := worker.DeleteActor(ctx, "federation-account", connector.ActorDeleteCommand{ActorID: ownedActor.ID})
	if err != nil {
		t.Fatalf("delete account-owned Actor: %v", err)
	}
	if deletedOwned.ActorID != ownedActor.ID || deletedOwned.URI != ownedActor.URI || deletedOwned.DeletedAt.IsZero() {
		t.Fatalf("unexpected deleted Actor result: %+v", deletedOwned)
	}
	if active, findErr := actorRepo.FindOwnedLocalByID(ctx, "federation-account", ownedActor.ID); findErr != nil || active != nil {
		t.Fatalf("deleted account-owned Actor remains active: actor=%+v err=%v", active, findErr)
	}
	if status := misskey.getStatus(ctx, ownedActor.URI); status != http.StatusNotFound {
		t.Fatalf("deleted Actor ActivityPub endpoint status=%d, want %d", status, http.StatusNotFound)
	}
	waitFor(t, ctx, "Misskey applies Delete(Actor)", func() bool {
		status := misskey.callStatus(ctx, "users/show", map[string]any{
			"i": directRecipient.Token, "userId": ownedMisskeyID,
		}, nil)
		return status == http.StatusNotFound
	})

	// Phase 24: subscribe through one Rosmarinus Redis broker and publish through
	// another, verifying multi-process SSE fan-out reaches only the intended
	// account and never crosses the per-account isolation boundary.
	redisA := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer redisA.Close()
	redisB := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer redisB.Close()
	brokerA := realtime.NewRedisBroker(redisA)
	brokerB := realtime.NewRedisBroker(redisB)
	accountA, err := brokerA.Subscribe(ctx, "sse-account-a")
	if err != nil {
		t.Fatalf("subscribe first SSE account: %v", err)
	}
	defer accountA.Close()
	accountB, err := brokerA.Subscribe(ctx, "sse-account-b")
	if err != nil {
		t.Fatalf("subscribe second SSE account: %v", err)
	}
	defer accountB.Close()
	if err := brokerB.Publish(ctx, "sse-account-a", "projection.invalidated", ownedActor.ID, map[string]string{"resource": "timeline"}); err != nil {
		t.Fatalf("publish cross-process SSE invalidation: %v", err)
	}
	select {
	case payload := <-accountA.Channel():
		var event realtime.Event
		if err := json.Unmarshal(payload, &event); err != nil || event.Type != "projection.invalidated" || event.ActorID != ownedActor.ID {
			t.Fatalf("unexpected SSE invalidation: event=%+v err=%v payload=%s", event, err, payload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for cross-process SSE invalidation")
	}
	select {
	case payload := <-accountB.Channel():
		t.Fatalf("SSE event crossed account boundary: %s", payload)
	case <-time.After(250 * time.Millisecond):
	}
}
