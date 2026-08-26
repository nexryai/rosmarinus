package federation_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	apclient "github.com/nexryai/rosmarinus/internal/activitypub/client"
	apworker "github.com/nexryai/rosmarinus/internal/activitypub/worker"
	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/connector"
	"github.com/nexryai/rosmarinus/internal/domain/emojis"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/domain/reactions"
	instancemetadata "github.com/nexryai/rosmarinus/internal/instance"
	"github.com/nexryai/rosmarinus/internal/queue"
	mongostore "github.com/nexryai/rosmarinus/internal/store/mongo"
)

func TestLatestMisskeyFederationWorkflows(t *testing.T) {
	if os.Getenv("ROSMARINUS_FEDERATION_TEST") != "1" {
		t.Skip("set ROSMARINUS_FEDERATION_TEST=1 inside the federation fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Phase 1: connect to the real MongoDB/Redis fixture, load Rosmarinus's
	// local Actor, and create two Misskey accounts for public and direct flows.
	cfg := config.Config{
		Host:          "rosmarinus.test",
		PublicURL:     "https://rosmarinus.test",
		MongoURI:      envRequired(t, "MONGO_URI"),
		MongoDatabase: envRequired(t, "MONGO_DATABASE"),
		RedisAddr:     envRequired(t, "REDIS_ADDR"),
		UserAgent:     "rosmarinus-federation-test/1.0",
		InboxQueue:    config.QueueConfig{Name: queue.QueueInbox, MaxRetry: 7, Timeout: 5 * time.Minute},
		DeliverQueue:  config.QueueConfig{Name: queue.QueueDeliver, MaxRetry: 11, Timeout: time.Minute},
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
	reactionRepo := mongostore.NewReactionRepository(db)
	pollRepo := mongostore.NewPollRepository(db)
	emojiRepo := mongostore.NewEmojiRepository(db)
	instanceRepo := mongostore.NewInstanceRepository(db)

	localActor, err := actorRepo.FindLocalByUsername(ctx, "relay")
	if err != nil || localActor == nil {
		t.Fatalf("find local relay actor: actor=%+v err=%v", localActor, err)
	}
	t.Logf("federation fixture ready local_actor_id=%s local_actor_uri=%s", localActor.ID, localActor.URI)

	misskey := newMisskeyClient(t)
	admin := misskey.createAdmin(ctx, "federationadmin", "federation-password")
	t.Logf("Misskey test account created actor_id=%s", admin.ID)
	directRecipient := misskey.createAccount(ctx, admin.Token, "directrecipient", "direct-password")
	t.Logf("Misskey direct recipient created actor_id=%s", directRecipient.ID)
	misskey.call(ctx, "admin/update-meta", map[string]any{
		"i":          admin.Token,
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
		mongostore.NewBlockRepository(db),
		reactionRepo,
		mongostore.NewReportRepository(db),
		queueClient,
		client,
		localActor,
	)
	worker.SetPollRepository(pollRepo)
	worker.SetEmojiRepository(emojiRepo)
	worker.SetAccountCleanupRepository(mongostore.NewAccountCleanupRepository(db))
	worker.SetInstanceRepository(instanceRepo, instancemetadata.New(30*time.Second, cfg.UserAgent, nil, misskey.httpClient))

	// Phase 2: send an outgoing Follow to Misskey, verify that its dereferenceable
	// Follow resource is exposed while pending, then wait for Misskey's Accept.
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
	}, nil)
	waitFor(t, ctx, "Update(Person) stored by Rosmarinus", func() bool {
		var findErr error
		remoteActor, findErr = actorRepo.FindByURI(ctx, remoteActorURI)
		return findErr == nil && remoteActor != nil && remoteActor.Name == updatedRemoteActorName && remoteActor.AvatarURL != ""
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

	// Phase 5: publish a public Misskey note and verify Rosmarinus accepts,
	// verifies, and persists the delivered Create(Note).
	var created struct {
		CreatedNote struct {
			ID string `json:"id"`
		} `json:"createdNote"`
	}
	misskey.call(ctx, "notes/create", map[string]any{
		"i":    admin.Token,
		"text": "Hello from latest Misskey federation test",
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
		return findErr == nil && remoteNote != nil && remoteNote.Text == "Hello from latest Misskey federation test"
	})

	// Phase 6: renote the public Misskey note and verify Rosmarinus accepts the
	// delivered Announce and stores its reference to the resolved target Note.
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

	// Phase 7: publish a Misskey Question and verify Rosmarinus stores its
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

	// Phase 8: react to the Misskey note as the local Actor, verify Misskey
	// applies the delivered Like, dereference it, then deliver Undo(Like) and
	// verify Misskey removes the reaction.
	createdReaction, err := worker.CreateReaction(ctx, connector.ReactionCreateCommand{
		ActorID:  localActor.ID,
		NoteID:   remoteNote.ID,
		Reaction: "👍",
	})
	if err != nil {
		t.Fatalf("create Rosmarinus reaction: %v", err)
	}
	waitFor(t, ctx, "Rosmarinus reaction applied by Misskey", func() bool {
		var shown struct {
			Reactions map[string]int `json:"reactions"`
		}
		misskey.call(ctx, "notes/show", map[string]any{
			"i":      admin.Token,
			"noteId": created.CreatedNote.ID,
		}, &shown)
		return shown.Reactions["👍"] > 0
	})
	var outgoingLike map[string]any
	misskey.get(ctx, createdReaction.URI, &outgoingLike)
	if outgoingLike["type"] != "Like" || outgoingLike["actor"] != localActor.URI || outgoingLike["object"] != noteURI || outgoingLike["_misskey_reaction"] != "👍" {
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
		return shown.Reactions["👍"] == 0
	})

	// Phase 9: undo Rosmarinus's accepted outgoing Follow, verify its MongoDB
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

	// Phase 10: make Misskey follow Rosmarinus, approve the pending request in
	// Rosmarinus, and verify Misskey applies the delivered Accept(Follow).
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

	// Phase 11: publish simple and advanced MFM from Rosmarinus, verify simple
	// MFM is safe HTML without redundant source metadata, advanced MFM retains
	// its source, and confirm latest Misskey stores both delivered Create(Note)s.
	const localNoteID = "latest-misskey-outbound-note"
	const localNoteText = "Hello from Rosmarinus federation delivery :party:"
	const misskeyLocalNoteText = "Hello from Rosmarinus federation delivery \u200B:party:\u200B"
	createdLocal, err := worker.CreatePost(ctx, connector.PostCreateCommand{
		ActorID:    localActor.ID,
		NoteID:     localNoteID,
		Text:       localNoteText,
		EmojiNames: []string{"party"},
		Visibility: string(domainnotes.VisibilityPublic),
		Poll:       &connector.PollCreateCommand{Choices: []string{"cats", "dogs"}},
	})
	if err != nil {
		t.Fatalf("create local Rosmarinus post: %v", err)
	}
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
	waitFor(t, ctx, "Create(Note) stored by Misskey", func() bool {
		var notes []struct {
			ID   string `json:"id"`
			Text string `json:"text"`
			URI  string `json:"uri"`
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
				return true
			}
		}
		return false
	})
	if storedLocalNoteText != misskeyLocalNoteText {
		t.Fatalf("Misskey stored simple MFM text %q, want %q", storedLocalNoteText, misskeyLocalNoteText)
	}

	const advancedNoteID = "latest-misskey-outbound-advanced-mfm"
	const advancedNoteText = "**Hello from Rosmarinus** $[ruby 漢字 かんじ]"
	advancedLocal, err := worker.CreatePost(ctx, connector.PostCreateCommand{
		ActorID: localActor.ID, NoteID: advancedNoteID, Text: advancedNoteText,
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

	// Phase 12: vote on the delivered Rosmarinus Question from Misskey and
	// verify Rosmarinus consumes the reply Note as a poll vote.
	misskey.call(ctx, "notes/polls/vote", map[string]any{
		"i": admin.Token, "noteId": misskeyLocalNoteID, "choice": 1,
	}, nil)
	waitFor(t, ctx, "Misskey poll vote stored by Rosmarinus", func() bool {
		poll, findErr := pollRepo.FindByNoteID(ctx, localNoteID)
		return findErr == nil && poll != nil && len(poll.Votes) == 2 && poll.Votes[1] == 1
	})

	// Phase 13: react to the delivered Rosmarinus note from Misskey, verify
	// Rosmarinus stores the federated reaction, and dereference its Like activity.
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
	likeActivityURI := cfg.PublicURL + "/likes/" + url.PathEscape(storedReaction.ID)
	var likeActivity map[string]any
	misskey.get(ctx, likeActivityURI, &likeActivity)
	if likeActivity["type"] != "Like" || likeActivity["actor"] != remoteActorURI || likeActivity["object"] != createdLocal.URI || likeActivity["_misskey_reaction"] != "👍" {
		t.Fatalf("unexpected Like activity: %#v", likeActivity)
	}

	// Phase 14: soft-delete the local Note, verify Rosmarinus removes its Poll,
	// votes, and reactions, and verify Misskey applies the delivered Tombstone.
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
		misskey.call(ctx, "users/notes", map[string]any{
			"i": admin.Token, "userId": relayOnMisskey.ID, "limit": 10,
		}, &notes)
		for _, note := range notes {
			if note.URI == createdLocal.URI {
				return false
			}
		}
		return true
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

	// Phase 15: publish a specified-visibility note, verify it is not publicly
	// dereferenceable, and confirm that the non-following Misskey recipient
	// still receives it through the individual inbox delivery.
	const specifiedNoteID = "latest-misskey-specified-note"
	const specifiedNoteText = "Private hello from Rosmarinus"
	directRecipientURI := "https://a.test/users/" + directRecipient.ID
	createdSpecified, err := worker.CreatePost(ctx, connector.PostCreateCommand{
		ActorID:     localActor.ID,
		NoteID:      specifiedNoteID,
		Text:        specifiedNoteText,
		Visibility:  string(domainnotes.VisibilitySpecified),
		MentionURIs: []string{directRecipientURI},
	})
	if err != nil {
		t.Fatalf("create specified Rosmarinus post: %v", err)
	}
	t.Logf("Rosmarinus specified note created note_id=%s uri=%s recipient=%s", createdSpecified.NoteID, createdSpecified.URI, directRecipientURI)
	if status := misskey.getStatus(ctx, createdSpecified.URI+"/activity"); status != http.StatusNotFound {
		t.Fatalf("specified Create activity status = %d, want %d", status, http.StatusNotFound)
	}
	waitFor(t, ctx, "specified Create(Note) stored for Misskey recipient", func() bool {
		var notes []struct {
			Text string `json:"text"`
			URI  string `json:"uri"`
		}
		misskey.call(ctx, "users/notes", map[string]any{
			"i":      directRecipient.Token,
			"userId": relayOnMisskey.ID,
			"limit":  10,
		}, &notes)
		for _, note := range notes {
			if note.Text == specifiedNoteText && note.URI == createdSpecified.URI {
				return true
			}
		}
		return false
	})

	// Phase 16: verify first contact, authenticated inbox traffic, successful
	// deliveries, relationship changes, and daily NodeInfo discovery converge on
	// one current-Misskey instance document that Salvia can read.
	waitFor(t, ctx, "Misskey instance metadata and federation stats stored", func() bool {
		instance, findErr := instanceRepo.FindByHost(ctx, "a.test")
		return findErr == nil && instance != nil && instance.SoftwareName == "misskey" &&
			instance.InfoUpdatedAt != nil && instance.LatestRequestReceivedAt != nil &&
			instance.LatestRequestSentAt != nil && instance.LatestStatus >= 200 && instance.LatestStatus < 300 &&
			!instance.IsNotResponding && instance.SuspensionState == "none" &&
			instance.UsersCount >= 2 && instance.FollowingCount == 1 && instance.FollowersCount == 0
	})
}

type misskeyClient struct {
	t          *testing.T
	baseURL    string
	httpClient *http.Client
}

type misskeyAccount struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}

type misskeyDriveFile struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

func newMisskeyClient(t *testing.T) *misskeyClient {
	t.Helper()
	return &misskeyClient{t: t, baseURL: "https://a.test", httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (m *misskeyClient) createAdmin(ctx context.Context, username, password string) misskeyAccount {
	var account misskeyAccount
	m.call(ctx, "admin/accounts/create", map[string]any{"username": username, "password": password}, &account)
	if account.ID == "" || account.Token == "" {
		m.t.Fatalf("Misskey admin creation returned incomplete account: %+v", account)
	}
	return account
}

func (m *misskeyClient) createAccount(ctx context.Context, adminToken, username, password string) misskeyAccount {
	var account misskeyAccount
	m.call(ctx, "admin/accounts/create", map[string]any{
		"i":        adminToken,
		"username": username,
		"password": password,
	}, &account)
	if account.ID == "" || account.Token == "" {
		m.t.Fatalf("Misskey account creation returned incomplete account: %+v", account)
	}
	return account
}

func (m *misskeyClient) uploadFile(ctx context.Context, token, filename string, data []byte) misskeyDriveFile {
	m.t.Helper()
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)
	if err := writer.WriteField("i", token); err != nil {
		m.t.Fatalf("write Misskey upload token: %v", err)
	}
	if err := writer.WriteField("force", "true"); err != nil {
		m.t.Fatalf("write Misskey upload option: %v", err)
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		m.t.Fatalf("create Misskey upload part: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		m.t.Fatalf("write Misskey upload file: %v", err)
	}
	if err := writer.Close(); err != nil {
		m.t.Fatalf("close Misskey upload body: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/api/drive/files/create", &requestBody)
	if err != nil {
		m.t.Fatalf("create Misskey upload request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res, err := m.httpClient.Do(req)
	if err != nil {
		m.t.Fatalf("upload Misskey file: %v", err)
	}
	defer res.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		m.t.Fatalf("read Misskey upload response: %v", err)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		m.t.Fatalf("Misskey file upload status=%d body=%s", res.StatusCode, loggableMisskeyResponse(responseBody))
	}
	var file misskeyDriveFile
	if err := json.Unmarshal(responseBody, &file); err != nil || file.ID == "" {
		m.t.Fatalf("decode Misskey upload response: file=%+v err=%v", file, err)
	}
	return file
}

func (m *misskeyClient) call(ctx context.Context, endpoint string, payload map[string]any, result any) {
	m.t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		m.t.Fatalf("marshal Misskey %s request: %v", endpoint, err)
	}
	m.t.Logf("Misskey API request endpoint=%s body=%s", endpoint, loggableMisskeyResponse(body))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/api/"+endpoint, bytes.NewReader(body))
	if err != nil {
		m.t.Fatalf("create Misskey %s request: %v", endpoint, err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := m.httpClient.Do(req)
	if err != nil {
		m.t.Fatalf("call Misskey %s: %v", endpoint, err)
	}
	defer res.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		m.t.Fatalf("read Misskey %s response: %v", endpoint, err)
	}
	logBody := loggableMisskeyResponse(responseBody)
	m.t.Logf("Misskey API response endpoint=%s status=%s body=%s", endpoint, res.Status, logBody)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		m.t.Fatalf("Misskey %s status=%d body=%s", endpoint, res.StatusCode, logBody)
	}
	if result != nil && len(responseBody) > 0 {
		if err := json.Unmarshal(responseBody, result); err != nil {
			m.t.Fatalf("decode Misskey %s response: %v body=%s", endpoint, err, logBody)
		}
	}
}

func (m *misskeyClient) get(ctx context.Context, uri string, result any) {
	m.t.Helper()
	m.t.Logf("ActivityPub GET request uri=%s", uri)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		m.t.Fatalf("create ActivityPub GET request: %v", err)
	}
	req.Header.Set("Accept", "application/activity+json")
	res, err := m.httpClient.Do(req)
	if err != nil {
		m.t.Fatalf("ActivityPub GET %s: %v", uri, err)
	}
	defer res.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		m.t.Fatalf("read ActivityPub GET response: %v", err)
	}
	m.t.Logf("ActivityPub GET response uri=%s status=%s body=%s", uri, res.Status, loggableMisskeyResponse(responseBody))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		m.t.Fatalf("ActivityPub GET %s status=%d body=%s", uri, res.StatusCode, loggableMisskeyResponse(responseBody))
	}
	if err := json.Unmarshal(responseBody, result); err != nil {
		m.t.Fatalf("decode ActivityPub GET response: %v", err)
	}
}

func (m *misskeyClient) getStatus(ctx context.Context, uri string) int {
	m.t.Helper()
	m.t.Logf("ActivityPub status request uri=%s", uri)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		m.t.Fatalf("create ActivityPub status request: %v", err)
	}
	req.Header.Set("Accept", "application/activity+json")
	res, err := m.httpClient.Do(req)
	if err != nil {
		m.t.Fatalf("ActivityPub status request %s: %v", uri, err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	m.t.Logf("ActivityPub status response uri=%s status=%s", uri, res.Status)
	return res.StatusCode
}

func (m *misskeyClient) getRaw(ctx context.Context, uri string) (int, string, []byte) {
	m.t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		m.t.Fatalf("create raw GET request: %v", err)
	}
	res, err := m.httpClient.Do(req)
	if err != nil {
		m.t.Fatalf("raw GET %s: %v", uri, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		m.t.Fatalf("read raw GET %s: %v", uri, err)
	}
	return res.StatusCode, res.Header.Get("Content-Type"), body
}

func loggableMisskeyResponse(body []byte) string {
	if len(body) == 0 {
		return "<empty>"
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return string(body)
	}
	redactMisskeySecrets(value)
	var redacted bytes.Buffer
	encoder := json.NewEncoder(&redacted)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "<unavailable>"
	}
	return strings.TrimSpace(redacted.String())
}

func redactMisskeySecrets(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			switch strings.ToLower(key) {
			case "token", "accesstoken", "secret", "password", "i":
				typed[key] = "<redacted>"
			default:
				redactMisskeySecrets(child)
			}
		}
	case []any:
		for _, child := range typed {
			redactMisskeySecrets(child)
		}
	}
}

func waitFor(t *testing.T, ctx context.Context, description string, ready func() bool) {
	t.Helper()
	startedAt := time.Now()
	attempts := 0
	t.Logf("waiting for %s", description)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		attempts++
		if ready() {
			t.Logf("wait completed description=%q attempts=%d elapsed=%s", description, attempts, time.Since(startedAt).Round(time.Millisecond))
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %s: %v", description, ctx.Err())
		case <-ticker.C:
		}
	}
}

func envRequired(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("%s is required", name)
	}
	return value
}
