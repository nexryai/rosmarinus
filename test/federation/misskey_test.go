package federation_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	apclient "github.com/nexryai/rosmarinus/internal/activitypub/client"
	apworker "github.com/nexryai/rosmarinus/internal/activitypub/worker"
	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/connector"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/emojis"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
	instancemetadata "github.com/nexryai/rosmarinus/internal/instance"
	"github.com/nexryai/rosmarinus/internal/queue"
	"github.com/nexryai/rosmarinus/internal/readmodel"
	"github.com/nexryai/rosmarinus/internal/realtime"
	mongostore "github.com/nexryai/rosmarinus/internal/store/mongo"
)

func TestLatestMisskeyFederationWorkflows(t *testing.T) {
	if os.Getenv("ROSMARINUS_FEDERATION_TEST") != "1" {
		t.Skip("set ROSMARINUS_FEDERATION_TEST=1 inside the federation fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Phase 1: connect to the explicitly allowlisted private Docker federation
	// network, load Rosmarinus's local Actor, and create two Misskey accounts for
	// public and direct flows, verifying the persisted local Actor uses an
	// ObjectID-string ID and safe network policy permits the controlled topology.
	cfg := config.Config{
		Host:          "rosmarinus.test",
		PublicURL:     "https://rosmarinus.test",
		MongoURI:      envRequired(t, "MONGO_URI"),
		MongoDatabase: envRequired(t, "MONGO_DATABASE"),
		RedisAddr:     envRequired(t, "REDIS_ADDR"),
		UserAgent:     "rosmarinus-federation-test/1.0",
		MediaAllowedPrivateNetworks: []string{
			"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
		},
		InboxActivityReceiptTTL: 7 * 24 * time.Hour,
		InboxQueue:              config.QueueConfig{Name: queue.QueueInbox, MaxRetry: 7, Timeout: 5 * time.Minute},
		DeliverQueue:            config.QueueConfig{Name: queue.QueueDeliver, MaxRetry: 11, Timeout: time.Minute},
	}

	mongoClient, err := mongo.Connect(options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		t.Fatalf("connect MongoDB: %v", err)
	}
	defer mongoClient.Disconnect(context.Background())
	db := mongoClient.Database(cfg.MongoDatabase)
	actorRepo := mongostore.NewActorRepository(db)
	noteRepo := mongostore.NewNoteRepository(db)
	followRepo := mongostore.NewFollowRepository(db)
	blockRepo := mongostore.NewBlockRepository(db)
	reactionRepo := mongostore.NewReactionRepository(db)
	pollRepo := mongostore.NewPollRepository(db)
	mediaRepo := mongostore.NewMediaRepository(db)
	emojiRepo := mongostore.NewEmojiRepository(db)
	instanceRepo := mongostore.NewInstanceRepository(db)
	activityReceiptRepo := mongostore.NewActivityReceiptRepository(db)

	localActor, err := actorRepo.FindLocalByUsername(ctx, "relay")
	if err != nil || localActor == nil {
		t.Fatalf("find local relay actor: actor=%+v err=%v", localActor, err)
	}
	assertObjectIDString(t, "local Actor", localActor.ID)
	t.Logf("federation fixture ready local_actor_id=%s local_actor_uri=%s", localActor.ID, localActor.URI)

	misskey := newMisskeyClient(t)
	admin := misskey.createAdmin(ctx, "federationadmin", "federation-password")
	t.Logf("Misskey test account created actor_id=%s", admin.ID)
	misskeyB := newMisskeyClientAt(t, "https://b.test")
	adminB := misskeyB.createAdmin(ctx, "concernedremote", "federation-password")
	t.Logf("second Misskey test account created actor_id=%s", adminB.ID)
	directRecipient := misskey.createAccount(ctx, admin.Token, "directrecipient", "direct-password")
	directRecipientURI := "https://a.test/users/" + directRecipient.ID
	t.Logf("Misskey direct recipient created actor_id=%s", directRecipient.ID)
	misskey.call(ctx, "admin/update-meta", map[string]any{
		"i":          admin.Token,
		"federation": "all",
	}, nil)
	misskeyB.call(ctx, "admin/update-meta", map[string]any{
		"i":          adminB.Token,
		"federation": "all",
	}, nil)

	queueClient := queue.NewAsynqClient(queue.RedisConfig{Addr: cfg.RedisAddr})
	defer queueClient.Close()
	client := apclient.New(cfg, misskey.httpClient)
	worker := apworker.New(
		cfg,
		log.New(io.Discard, "", 0),
		actorRepo,
		noteRepo,
		followRepo,
		blockRepo,
		reactionRepo,
		mongostore.NewReportRepository(db),
		queueClient,
		client,
		localActor,
	)
	worker.SetPollRepository(pollRepo)
	worker.SetEmojiRepository(emojiRepo)
	worker.SetActivityReceiptRepository(activityReceiptRepo)
	worker.SetAccountCleanupRepository(mongostore.NewAccountCleanupRepository(db))
	worker.SetInstanceRepository(instanceRepo, instancemetadata.New(30*time.Second, cfg.UserAgent, nil, misskey.httpClient))

	// Phase 2: send an outgoing Follow to Misskey, verify that its dereferenceable
	// Follow resource is exposed while pending, then wait for Misskey's Accept.
	// The resulting non-empty following collection remains private over public
	// ActivityPub HTTP.
	remoteActorURI := "https://a.test/users/" + admin.ID
	result, err := worker.CreateFollow(ctx, localActor.ID, remoteActorURI)
	t.Logf("Rosmarinus outgoing Follow result=%q actor=%s target=%s err=%v", result, localActor.ID, remoteActorURI, err)
	if err != nil {
		t.Fatalf("create outgoing Follow: result=%q err=%v", result, err)
	}
	remoteActor, err := actorRepo.FindByURI(ctx, remoteActorURI)
	if err != nil || remoteActor == nil {
		t.Fatalf("find resolved Misskey actor: actor=%+v err=%v", remoteActor, err)
	}
	assertObjectIDString(t, "remote Actor", remoteActor.ID)
	storedFollow, err := followRepo.Find(ctx, localActor.ID, remoteActor.ID)
	if err != nil || storedFollow == nil {
		t.Fatalf("find outgoing Follow: follow=%+v err=%v", storedFollow, err)
	}
	assertObjectIDString(t, "Follow", storedFollow.ID)
	followActivityURI := cfg.PublicURL + "/follows/" + url.PathEscape(localActor.ID) + "/" + url.PathEscape(remoteActor.ID)
	var followActivity map[string]any
	misskey.get(ctx, followActivityURI, &followActivity)
	if followActivity["type"] != "Follow" || followActivity["actor"] != localActor.URI || followActivity["object"] != remoteActorURI {
		t.Fatalf("unexpected outgoing Follow activity: %#v", followActivity)
	}

	var relationship *follows.Follow
	waitFor(t, ctx, "Misskey Accept(Follow)", func() bool {
		var findErr error
		relationship, findErr = followRepo.Find(ctx, localActor.ID, remoteActor.ID)
		t.Logf("[DEBUG] followRepo.Find: %s <= %s: relationship=%+v err=%v", localActor.ID, remoteActor.ID, relationship, findErr)
		return findErr == nil && relationship != nil && relationship.Status == follows.StatusAccepted
	})
	if status := misskey.getStatus(ctx, localActor.URI+"/following"); status != http.StatusForbidden {
		t.Fatalf("public following collection status=%d, want %d", status, http.StatusForbidden)
	}
	if status := misskey.getStatus(ctx, localActor.URI+"/following?page=true"); status != http.StatusForbidden {
		t.Fatalf("public following page status=%d, want %d", status, http.StatusForbidden)
	}

	// Phase 3: upload an avatar and update the followed Misskey Actor, verifying
	// Rosmarinus authenticates Update(Person) and refreshes its profile fields.
	const updatedRemoteActorName = "Updated Misskey federation actor"
	avatarPNG, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatalf("decode avatar fixture: %v", err)
	}
	avatar := misskey.uploadFile(ctx, admin.Token, "avatar.png", avatarPNG)
	misskey.call(ctx, "i/update", map[string]any{
		"i": admin.Token, "name": updatedRemoteActorName, "avatarId": avatar.ID,
		"fields": []map[string]string{{"name": "Website", "value": "https://a.test"}},
	}, nil)
	waitFor(t, ctx, "Update(Person) stored by Rosmarinus", func() bool {
		var findErr error
		remoteActor, findErr = actorRepo.FindByURI(ctx, remoteActorURI)
		return findErr == nil && remoteActor != nil && remoteActor.Name == updatedRemoteActorName && remoteActor.AvatarURL != "" &&
			len(remoteActor.ProfileFields) == 1 && remoteActor.ProfileFields[0].Name == "Website" && remoteActor.ProfileFields[0].Value == "https://a.test"
	})

	// Phase 4: verify Rosmarinus preserves Misskey's validated direct avatar URL
	// and that the frontend-facing source resolves to an image without a backend
	// cache or image-processing step.
	status, avatarContentType, avatarBody := misskey.getRaw(ctx, remoteActor.AvatarURL)
	if status != http.StatusOK || !strings.HasPrefix(avatarContentType, "image/") || len(avatarBody) == 0 {
		t.Fatalf("direct avatar response status=%d content_type=%q bytes=%d", status, avatarContentType, len(avatarBody))
	}
	if _, err := emojiRepo.UpsertLocal(ctx, emojis.Emoji{
		Name: "party", URI: cfg.PublicURL + "/emojis/party",
		OriginalURL: remoteActor.AvatarURL, PublicURL: remoteActor.AvatarURL, MediaType: avatarContentType,
	}); err != nil {
		t.Fatalf("store local emoji fixture: %v", err)
	}
	var localEmoji map[string]any
	misskey.get(ctx, cfg.PublicURL+"/emojis/party", &localEmoji)
	if localEmoji["type"] != "Emoji" || localEmoji["name"] != ":party:" {
		t.Fatalf("unexpected local Emoji resource: %#v", localEmoji)
	}

	// Phase 5: publish a public Misskey note with an image and verify Rosmarinus
	// accepts, verifies, and persists the delivered Create(Note), including the
	// validated attachment URL and current-Misskey width/height metadata. Verify
	// the home timeline includes the followed remote author with profile fields.
	var created struct {
		CreatedNote struct {
			ID string `json:"id"`
		} `json:"createdNote"`
	}
	misskey.call(ctx, "notes/create", map[string]any{
		"i": admin.Token, "text": "Hello from latest Misskey federation test",
		"fileIds": []string{avatar.ID},
	}, &created)
	if created.CreatedNote.ID == "" {
		t.Fatal("Misskey notes/create returned an empty note id")
	}
	t.Logf("Misskey note created note_id=%s", created.CreatedNote.ID)

	noteURI := "https://a.test/notes/" + created.CreatedNote.ID
	var remoteNote *domainnotes.Note
	waitFor(t, ctx, "Create(Note) stored by Rosmarinus", func() bool {
		var findErr error
		remoteNote, findErr = noteRepo.FindByURI(ctx, noteURI)
		t.Logf("[DEBUG] noteRepo.FindByURI: %s: note=%+v err=%v", noteURI, remoteNote, findErr)
		return findErr == nil && remoteNote != nil && remoteNote.Text == "Hello from latest Misskey federation test" &&
			len(remoteNote.Attachments) == 1 && remoteNote.Attachments[0].URL != "" &&
			remoteNote.Attachments[0].Width == 1 && remoteNote.Attachments[0].Height == 1
	})
	assertObjectIDString(t, "remote Note", remoteNote.ID)
	home, err := mongostore.NewSalviaReader(db).ListHomeTimeline(ctx, localActor.ID, readmodel.Cursor{}, 30)
	if err != nil {
		t.Fatalf("read home timeline after Misskey Create: %v", err)
	}
	foundRemoteNote := false
	for _, item := range home {
		if item.Note.ID == remoteNote.ID {
			foundRemoteNote = true
			if item.Author == nil || item.Author.ID != remoteActor.ID || len(item.Author.ProfileFields) != 1 || item.Author.ProfileFields[0].Name != "Website" {
				t.Fatalf("home timeline lost remote author profile fields: %+v", item.Author)
			}
		}
	}
	if !foundRemoteNote {
		t.Fatal("home timeline omitted the followed Misskey user's note")
	}
	profileNotes, err := mongostore.NewSalviaReader(db).ListProfileNotes(ctx, localActor.ID, remoteActor.ID, readmodel.Cursor{}, 30)
	if err != nil {
		t.Fatalf("read remote Actor profile notes: %v", err)
	}
	foundRemoteNote = false
	for _, item := range profileNotes {
		if item.Note.ID == remoteNote.ID {
			foundRemoteNote = true
			break
		}
	}
	if !foundRemoteNote {
		t.Fatal("remote Actor profile omitted its visibility-checked Misskey note")
	}

	// Phase 6: pin and unpin the Misskey Note, verifying Rosmarinus applies the
	// delivered Add/Remove pair and exposes the visible Note through its profile
	// read model while pinned.
	misskey.call(ctx, "i/pin", map[string]any{
		"i": admin.Token, "noteId": created.CreatedNote.ID,
	}, nil)
	waitFor(t, ctx, "Add featured Note stored by Rosmarinus", func() bool {
		var findErr error
		remoteActor, findErr = actorRepo.FindByURI(ctx, remoteActorURI)
		if findErr != nil || remoteActor == nil {
			return false
		}
		for _, noteID := range remoteActor.FeaturedNoteIDs {
			if noteID == remoteNote.ID {
				return true
			}
		}
		return false
	})
	remoteProfile, err := mongostore.NewSalviaReader(db).FindProfile(ctx, localActor.ID, remoteActor.ID)
	if err != nil {
		t.Fatalf("read remote Actor profile with pinned Note: %v", err)
	}
	if remoteProfile == nil || len(remoteProfile.PinnedNotes) != 1 || remoteProfile.PinnedNotes[0].Note.ID != remoteNote.ID || len(remoteProfile.PinnedNotes[0].Note.Attachments) != 1 {
		t.Fatalf("remote Actor profile lost its visible pinned Note: %+v", remoteProfile)
	}
	misskey.call(ctx, "i/unpin", map[string]any{
		"i": admin.Token, "noteId": created.CreatedNote.ID,
	}, nil)
	waitFor(t, ctx, "Remove featured Note stored by Rosmarinus", func() bool {
		var findErr error
		remoteActor, findErr = actorRepo.FindByURI(ctx, remoteActorURI)
		if findErr != nil || remoteActor == nil {
			return false
		}
		for _, noteID := range remoteActor.FeaturedNoteIDs {
			if noteID == remoteNote.ID {
				return false
			}
		}
		return true
	})

	// Phase 7: renote and quote the public Misskey note, verifying Rosmarinus
	// accepts the delivered Announce and the Note containing Misskey's duplicate
	// `_misskey_quote`/`quoteUrl` fields, then stores resolved target references.
	var createdRenote struct {
		CreatedNote struct {
			ID string `json:"id"`
		} `json:"createdNote"`
	}
	misskey.call(ctx, "notes/create", map[string]any{
		"i":        admin.Token,
		"renoteId": created.CreatedNote.ID,
	}, &createdRenote)
	if createdRenote.CreatedNote.ID == "" {
		t.Fatal("Misskey renote returned an empty note id")
	}
	announceURI := "https://a.test/notes/" + createdRenote.CreatedNote.ID + "/activity"
	waitFor(t, ctx, "Announce stored by Rosmarinus", func() bool {
		announce, findErr := noteRepo.FindByURI(ctx, announceURI)
		return findErr == nil && announce != nil && announce.RenoteID == remoteNote.ID && announce.RenoteURI == remoteNote.URI
	})
	var createdQuote struct {
		CreatedNote struct {
			ID string `json:"id"`
		} `json:"createdNote"`
	}
	misskey.call(ctx, "notes/create", map[string]any{
		"i": admin.Token, "text": "Quoted from latest Misskey federation test",
		"renoteId": created.CreatedNote.ID,
	}, &createdQuote)
	if createdQuote.CreatedNote.ID == "" {
		t.Fatal("Misskey quote returned an empty note id")
	}
	quoteNoteURI := "https://a.test/notes/" + createdQuote.CreatedNote.ID
	waitFor(t, ctx, "Misskey quote Note stored with resolved target", func() bool {
		quote, findErr := noteRepo.FindByURI(ctx, quoteNoteURI)
		return findErr == nil && quote != nil && quote.Text == "Quoted from latest Misskey federation test" &&
			quote.QuoteID == remoteNote.ID && quote.QuoteURI == remoteNote.URI
	})

	// Phase 8: publish a Misskey Question and verify Rosmarinus stores its
	// ordered choices, initial vote counts, multiplicity, and expiration.
	var createdPoll struct {
		CreatedNote struct {
			ID string `json:"id"`
		} `json:"createdNote"`
	}
	misskey.call(ctx, "notes/create", map[string]any{
		"i":    admin.Token,
		"text": "Choose a federation test answer",
		"poll": map[string]any{
			"choices": []string{"cats", "dogs"}, "multiple": false,
			"expiresAt": time.Now().Add(10 * time.Minute).UnixMilli(),
		},
	}, &createdPoll)
	if createdPoll.CreatedNote.ID == "" {
		t.Fatal("Misskey poll returned an empty note id")
	}
	pollNoteURI := "https://a.test/notes/" + createdPoll.CreatedNote.ID
	waitFor(t, ctx, "Question poll stored by Rosmarinus", func() bool {
		note, findErr := noteRepo.FindByURI(ctx, pollNoteURI)
		if findErr != nil || note == nil {
			return false
		}
		poll, findErr := pollRepo.FindByNoteID(ctx, note.ID)
		return findErr == nil && poll != nil && !poll.Multiple && poll.ExpiresAt != nil &&
			len(poll.Choices) == 2 && poll.Choices[0] == "cats" && poll.Choices[1] == "dogs" &&
			len(poll.Votes) == 2 && poll.Votes[0] == 0 && poll.Votes[1] == 0
	})

	// Phase 9: react to the Misskey note with a local custom emoji, verify
	// Misskey applies the tagged Like and resolves its emoji image, dereference
	// it, then deliver Undo(Like) and verify Misskey removes the reaction.
	createdReaction, err := worker.CreateReaction(ctx, connector.ReactionCreateCommand{
		ActorID:  localActor.ID,
		NoteID:   remoteNote.ID,
		Reaction: ":party:",
	})
	if err != nil {
		t.Fatalf("create Rosmarinus reaction: %v", err)
	}
	waitFor(t, ctx, "Rosmarinus reaction applied by Misskey", func() bool {
		var shown struct {
			Reactions      map[string]int    `json:"reactions"`
			ReactionEmojis map[string]string `json:"reactionEmojis"`
		}
		misskey.call(ctx, "notes/show", map[string]any{
			"i":      admin.Token,
			"noteId": created.CreatedNote.ID,
		}, &shown)
		return shown.Reactions[":party@rosmarinus.test:"] > 0 && shown.ReactionEmojis["party@rosmarinus.test"] == remoteActor.AvatarURL
	})
	var outgoingLike map[string]any
	misskey.get(ctx, createdReaction.URI, &outgoingLike)
	likeTags, _ := outgoingLike["tag"].([]any)
	if outgoingLike["type"] != "Like" || outgoingLike["actor"] != localActor.URI || outgoingLike["object"] != noteURI || outgoingLike["_misskey_reaction"] != ":party@.:" || len(likeTags) != 1 {
		t.Fatalf("unexpected outgoing Like activity: %#v", outgoingLike)
	}
	deletedReaction, err := worker.DeleteReaction(ctx, connector.ReactionDeleteCommand{
		ActorID: localActor.ID,
		NoteID:  remoteNote.ID,
	})
	if err != nil {
		t.Fatalf("delete Rosmarinus reaction: %v", err)
	}
	if deletedReaction.ReactionID != createdReaction.ReactionID || deletedReaction.URI != createdReaction.URI+"/undo" {
		t.Fatalf("unexpected deleted reaction: %+v", deletedReaction)
	}
	waitFor(t, ctx, "Rosmarinus Undo(Like) applied by Misskey", func() bool {
		var shown struct {
			Reactions map[string]int `json:"reactions"`
		}
		misskey.call(ctx, "notes/show", map[string]any{
			"i":      admin.Token,
			"noteId": created.CreatedNote.ID,
		}, &shown)
		return shown.Reactions[":party@rosmarinus.test:"] == 0
	})

	// Phase 10: undo Rosmarinus's accepted outgoing Follow, verify its MongoDB
	// relationship is soft-deleted, and confirm Misskey removes the relay Actor
	// from the administrator's followers.
	var relayOnMisskey struct {
		ID          string `json:"id"`
		IsFollowing bool   `json:"isFollowing"`
	}
	misskey.call(ctx, "users/show", map[string]any{
		"i":        admin.Token,
		"username": "relay",
		"host":     "rosmarinus.test",
	}, &relayOnMisskey)
	if relayOnMisskey.ID == "" {
		t.Fatal("Misskey users/show returned an empty Rosmarinus actor id")
	}
	t.Logf("Misskey resolved Rosmarinus actor misskey_user_id=%s", relayOnMisskey.ID)
	waitFor(t, ctx, "Rosmarinus relay appears in Misskey followers", func() bool {
		var followers []struct {
			Follower struct {
				ID  string `json:"id"`
				URI string `json:"uri"`
			} `json:"follower"`
		}
		misskey.call(ctx, "users/followers", map[string]any{
			"i":      admin.Token,
			"userId": admin.ID,
			"limit":  100,
		}, &followers)
		for _, follower := range followers {
			if follower.Follower.ID == relayOnMisskey.ID || follower.Follower.URI == localActor.URI {
				return true
			}
		}
		return false
	})
	deletedFollow, err := worker.DeleteFollow(ctx, connector.FollowDeleteCommand{
		ActorID: localActor.ID,
		Target:  remoteActorURI,
	})
	if err != nil {
		t.Fatalf("delete outgoing Follow: %v", err)
	}
	if deletedFollow.FollowerID != localActor.ID || deletedFollow.FolloweeID != remoteActor.ID || deletedFollow.URI != followActivityURI+"/undo" {
		t.Fatalf("unexpected deleted follow: %+v", deletedFollow)
	}
	waitFor(t, ctx, "outgoing Follow soft-deleted in Rosmarinus", func() bool {
		stored, findErr := followRepo.Find(ctx, localActor.ID, remoteActor.ID)
		return findErr == nil && stored == nil
	})
	waitFor(t, ctx, "Rosmarinus relay removed from Misskey followers", func() bool {
		var followers []struct {
			Follower struct {
				ID  string `json:"id"`
				URI string `json:"uri"`
			} `json:"follower"`
		}
		misskey.call(ctx, "users/followers", map[string]any{
			"i":      admin.Token,
			"userId": admin.ID,
			"limit":  100,
		}, &followers)
		for _, follower := range followers {
			if follower.Follower.ID == relayOnMisskey.ID || follower.Follower.URI == localActor.URI {
				return false
			}
		}
		return true
	})

	// Phase 11: make Misskey follow Rosmarinus, approve the pending request in
	// Rosmarinus, and verify Misskey applies the delivered Accept(Follow). The
	// resulting non-empty followers collection remains private over public
	// ActivityPub HTTP.
	misskey.call(ctx, "following/create", map[string]any{
		"i":      admin.Token,
		"userId": relayOnMisskey.ID,
	}, nil)

	waitFor(t, ctx, "inbound Misskey Follow stored as pending", func() bool {
		inbound, findErr := followRepo.Find(ctx, remoteActor.ID, localActor.ID)
		return findErr == nil && inbound != nil && inbound.Status == follows.StatusPending
	})
	result, err = worker.ApproveFollow(ctx, remoteActor.ID, localActor.ID)
	t.Logf("Rosmarinus inbound Follow approval result=%q follower=%s followee=%s err=%v", result, remoteActor.ID, localActor.ID, err)
	if err != nil {
		t.Fatalf("approve inbound Misskey Follow: result=%q err=%v", result, err)
	}
	waitFor(t, ctx, "Misskey applies Accept(Follow)", func() bool {
		var shown struct {
			IsFollowing bool `json:"isFollowing"`
		}
		misskey.call(ctx, "users/show", map[string]any{
			"i":      admin.Token,
			"userId": relayOnMisskey.ID,
		}, &shown)
		return shown.IsFollowing
	})
	if status := misskey.getStatus(ctx, localActor.URI+"/followers"); status != http.StatusForbidden {
		t.Fatalf("public followers collection status=%d, want %d", status, http.StatusForbidden)
	}
	if status := misskey.getStatus(ctx, localActor.URI+"/followers?page=true"); status != http.StatusForbidden {
		t.Fatalf("public followers page status=%d, want %d", status, http.StatusForbidden)
	}

	// Phase 12: create an account-owned Actor, exercise follows in both
	// directions, and update its profile. Verify Rosmarinus delivers a full
	// Update(Person), then verify an inbound Misskey home Note publishes a
	// note.created SSE invalidation to the following Actor's owning account.
	// Add a sibling Actor and prove blocking from either account Actor applies
	// and federates the same target for both, then remove the sibling fixture.
	const ownedUsername = "ownedprofile"
	const ownedUpdatedName = "Updated Rosmarinus profile"
	ownedCreated, err := worker.CreateActor(ctx, "federation-account", connector.ActorCreateCommand{
		Username: ownedUsername,
		Name:     "Rosmarinus owned profile",
		Type:     "Person",
	})
	if err != nil {
		t.Fatalf("create account-owned Actor: %v", err)
	}
	ownedActor, err := actorRepo.FindOwnedLocalByID(ctx, "federation-account", ownedCreated.ActorID)
	if err != nil || ownedActor == nil {
		t.Fatalf("find account-owned Actor: actor=%+v err=%v", ownedActor, err)
	}
	var ownedOnMisskey struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		IsBot bool   `json:"isBot"`
		IsCat bool   `json:"isCat"`
	}
	misskey.call(ctx, "users/show", map[string]any{
		"i": directRecipient.Token, "username": ownedUsername, "host": "rosmarinus.test",
	}, &ownedOnMisskey)
	if ownedOnMisskey.ID == "" {
		t.Fatal("Misskey did not resolve the account-owned Rosmarinus Actor")
	}
	ownedMisskeyID := ownedOnMisskey.ID
	misskey.call(ctx, "following/create", map[string]any{
		"i": directRecipient.Token, "userId": ownedMisskeyID,
	}, nil)
	var directRecipientActor *actors.Actor
	waitFor(t, ctx, "owned Actor inbound Follow stored as pending", func() bool {
		var findErr error
		directRecipientActor, findErr = actorRepo.FindByURI(ctx, directRecipientURI)
		if findErr != nil || directRecipientActor == nil {
			return false
		}
		inbound, findErr := followRepo.Find(ctx, directRecipientActor.ID, ownedActor.ID)
		return findErr == nil && inbound != nil && inbound.Status == follows.StatusPending
	})
	if _, err := worker.ApproveFollow(ctx, directRecipientActor.ID, ownedActor.ID); err != nil {
		t.Fatalf("approve owned Actor Follow: %v", err)
	}
	waitFor(t, ctx, "owned Actor Follow accepted by Misskey", func() bool {
		var shown struct {
			IsFollowing bool `json:"isFollowing"`
		}
		misskey.call(ctx, "users/show", map[string]any{
			"i": directRecipient.Token, "userId": ownedMisskeyID,
		}, &shown)
		return shown.IsFollowing
	})
	updatedOwned, err := worker.UpdateActor(ctx, "federation-account", connector.ActorUpdateCommand{
		ActorID: ownedActor.ID,
		Patch: connector.ActorUpdateData{
			Name:  ownedUpdatedName,
			IsBot: true,
			IsCat: true,
			Present: map[string]bool{
				"name": true, "is_bot": true, "is_cat": true,
			},
		},
	})
	if err != nil {
		t.Fatalf("update account-owned Actor: %v", err)
	}
	if updatedOwned.ActorID != ownedActor.ID || updatedOwned.URI != ownedActor.URI {
		t.Fatalf("unexpected updated Actor result: %+v", updatedOwned)
	}
	waitFor(t, ctx, "Update(Person) from Rosmarinus applied by Misskey", func() bool {
		ownedOnMisskey = struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			IsBot bool   `json:"isBot"`
			IsCat bool   `json:"isCat"`
		}{}
		misskey.call(ctx, "users/show", map[string]any{
			"i": directRecipient.Token, "userId": ownedMisskeyID,
		}, &ownedOnMisskey)
		return ownedOnMisskey.Name == ownedUpdatedName && ownedOnMisskey.IsBot && ownedOnMisskey.IsCat
	})
	if _, err := worker.CreateFollow(ctx, ownedActor.ID, remoteActorURI); err != nil {
		t.Fatalf("create account-owned Actor outgoing Follow: %v", err)
	}
	waitFor(t, ctx, "account-owned Actor Follow accepted by Misskey", func() bool {
		relationship, findErr := followRepo.Find(ctx, ownedActor.ID, remoteActor.ID)
		return findErr == nil && relationship != nil && relationship.Status == follows.StatusAccepted
	})
	eventRedis := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer eventRedis.Close()
	eventSubscription, err := realtime.NewRedisBroker(eventRedis).Subscribe(ctx, "federation-account")
	if err != nil {
		t.Fatalf("subscribe account-owned Actor SSE channel: %v", err)
	}
	defer eventSubscription.Close()
	var sseRemoteNote struct {
		CreatedNote struct {
			ID string `json:"id"`
		} `json:"createdNote"`
	}
	misskey.call(ctx, "notes/create", map[string]any{
		"i": admin.Token, "text": "Remote home note for Salvia SSE", "visibility": "home",
	}, &sseRemoteNote)
	if sseRemoteNote.CreatedNote.ID == "" {
		t.Fatal("Misskey SSE fixture note has an empty ID")
	}
	sseNoteURI := "https://a.test/notes/" + sseRemoteNote.CreatedNote.ID
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	receivedRemoteNoteID := ""
waitForRemoteNoteEvent:
	for {
		select {
		case payload, open := <-eventSubscription.Channel():
			if !open {
				t.Fatal("account-owned Actor SSE subscription closed before note.created")
			}
			var event realtime.Event
			if err := json.Unmarshal(payload, &event); err != nil {
				t.Fatalf("decode remote note SSE event: %v", err)
			}
			data, _ := event.Data.(map[string]any)
			noteID, _ := data["note_id"].(string)
			if event.Type == "note.created" && event.ActorID == ownedActor.ID && noteID != "" {
				receivedRemoteNoteID = noteID
				break waitForRemoteNoteEvent
			}
		case <-timer.C:
			t.Fatal("timed out waiting for remote note.created SSE event")
		}
	}
	var storedSSENote *domainnotes.Note
	waitFor(t, ctx, "SSE fixture Note stored by Rosmarinus", func() bool {
		var findErr error
		storedSSENote, findErr = noteRepo.FindByURI(ctx, sseNoteURI)
		return findErr == nil && storedSSENote != nil && storedSSENote.Text == "Remote home note for Salvia SSE"
	})
	if receivedRemoteNoteID != storedSSENote.ID {
		t.Fatalf("remote note.created ID = %q, stored Note ID = %q", receivedRemoteNoteID, storedSSENote.ID)
	}
	if _, err := worker.DeleteFollow(ctx, connector.FollowDeleteCommand{ActorID: ownedActor.ID, Target: remoteActorURI}); err != nil {
		t.Fatalf("remove account-owned Actor outgoing Follow fixture: %v", err)
	}
	const siblingUsername = "ownedsibling"
	siblingCreated, err := worker.CreateActor(ctx, "federation-account", connector.ActorCreateCommand{
		Username: siblingUsername,
		Name:     "Rosmarinus owned sibling",
		Type:     "Person",
	})
	if err != nil {
		t.Fatalf("create sibling account-owned Actor: %v", err)
	}
	siblingActor, err := actorRepo.FindOwnedLocalByID(ctx, "federation-account", siblingCreated.ActorID)
	if err != nil || siblingActor == nil {
		t.Fatalf("find sibling account-owned Actor: actor=%+v err=%v", siblingActor, err)
	}
	var siblingOnMisskey struct {
		ID string `json:"id"`
	}
	misskey.call(ctx, "users/show", map[string]any{
		"i": directRecipient.Token, "username": siblingUsername, "host": "rosmarinus.test",
	}, &siblingOnMisskey)
	if siblingOnMisskey.ID == "" {
		t.Fatal("Misskey did not resolve the sibling account-owned Actor")
	}
	sharedBlock, err := worker.CreateBlock(ctx, connector.BlockCreateCommand{ActorID: ownedActor.ID, Target: remoteActorURI})
	if err != nil {
		t.Fatalf("create account-shared Block: %v", err)
	}
	if sharedBlock.BlockeeID != remoteActor.ID {
		t.Fatalf("unexpected account-shared Block: %+v", sharedBlock)
	}
	assertObjectIDString(t, "Block", sharedBlock.BlockID)
	for _, blockerID := range []string{ownedActor.ID, siblingActor.ID} {
		if block, findErr := blockRepo.Find(ctx, blockerID, remoteActor.ID); findErr != nil || block == nil {
			t.Fatalf("account-shared Block missing for %s: block=%+v err=%v", blockerID, block, findErr)
		}
	}
	waitFor(t, ctx, "account-shared Blocks applied by Misskey", func() bool {
		for _, userID := range []string{ownedMisskeyID, siblingOnMisskey.ID} {
			var shown struct {
				IsBlocked bool `json:"isBlocked"`
			}
			misskey.call(ctx, "users/show", map[string]any{"i": admin.Token, "userId": userID}, &shown)
			if !shown.IsBlocked {
				return false
			}
		}
		return true
	})
	if _, err := worker.DeleteBlock(ctx, connector.BlockDeleteCommand{ActorID: siblingActor.ID, Target: remoteActorURI}); err != nil {
		t.Fatalf("delete account-shared Block from sibling: %v", err)
	}
	waitFor(t, ctx, "account-shared Undo(Blocks) applied by Misskey", func() bool {
		for _, userID := range []string{ownedMisskeyID, siblingOnMisskey.ID} {
			var shown struct {
				IsBlocked bool `json:"isBlocked"`
			}
			misskey.call(ctx, "users/show", map[string]any{"i": admin.Token, "userId": userID}, &shown)
			if shown.IsBlocked {
				return false
			}
		}
		return true
	})
	if _, err := worker.DeleteActor(ctx, "federation-account", connector.ActorDeleteCommand{ActorID: siblingActor.ID}); err != nil {
		t.Fatalf("delete sibling account-owned Actor fixture: %v", err)
	}

	// Phases 13-24 continue in a helper so this stateful fixture stays sequential
	// while each Go source file remains small enough to review safely.
	runLatestMisskeyFederationPhases13To24(t, misskeyFederationPhaseState{
		ctx:                 ctx,
		worker:              worker,
		cfg:                 cfg,
		db:                  db,
		actorRepo:           actorRepo,
		noteRepo:            noteRepo,
		followRepo:          followRepo,
		blockRepo:           blockRepo,
		reactionRepo:        reactionRepo,
		pollRepo:            pollRepo,
		mediaRepo:           mediaRepo,
		instanceRepo:        instanceRepo,
		activityReceiptRepo: activityReceiptRepo,
		localActor:          localActor,
		remoteActor:         remoteActor,
		remoteActorURI:      remoteActorURI,
		remoteNote:          remoteNote,
		remoteMisskeyNoteID: created.CreatedNote.ID,
		misskey:             misskey,
		admin:               admin,
		misskeyB:            misskeyB,
		adminB:              adminB,
		directRecipient:     directRecipient,
		directRecipientURI:  directRecipientURI,
		relayOnMisskeyID:    relayOnMisskey.ID,
		avatarPNG:           avatarPNG,
		ownedActor:          ownedActor,
		ownedMisskeyID:      ownedMisskeyID,
	})
}
