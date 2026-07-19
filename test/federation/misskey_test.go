package federation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	apclient "github.com/nexryai/rosmarinus/internal/activitypub/client"
	apworker "github.com/nexryai/rosmarinus/internal/activitypub/worker"
	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	"github.com/nexryai/rosmarinus/internal/queue"
	mongostore "github.com/nexryai/rosmarinus/internal/store/mongo"
)

func TestLatestMisskeyFollowAcceptAndNoteDelivery(t *testing.T) {
	if os.Getenv("ROSMARINUS_FEDERATION_TEST") != "1" {
		t.Skip("set ROSMARINUS_FEDERATION_TEST=1 inside the federation fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cfg := config.Config{
		Host:          "rosmarinus.test",
		PublicURL:     "https://rosmarinus.test",
		MongoURI:      envRequired(t, "MONGO_URI"),
		MongoDatabase: envRequired(t, "MONGO_DATABASE"),
		RedisAddr:     envRequired(t, "REDIS_ADDR"),
		UserAgent:     "rosmarinus-federation-test/1.0",
		InboxQueue:    config.QueueConfig{Name: queue.QueueInbox, MaxRetry: 10, Timeout: 5 * time.Minute},
		DeliverQueue:  config.QueueConfig{Name: queue.QueueDeliver, MaxRetry: 17, Timeout: time.Minute},
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

	localActor, err := actorRepo.FindLocalByUsername(ctx, "relay")
	if err != nil || localActor == nil {
		t.Fatalf("find local relay actor: actor=%+v err=%v", localActor, err)
	}

	misskey := newMisskeyClient(t)
	admin := misskey.createAdmin(ctx, "federationadmin", "federation-password")
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
		mongostore.NewReactionRepository(db),
		mongostore.NewReportRepository(db),
		queueClient,
		client,
		localActor,
	)

	remoteActorURI := "https://a.test/users/" + admin.ID
	if result, err := worker.CreateFollow(ctx, localActor.ID, remoteActorURI); err != nil {
		t.Fatalf("create outgoing Follow: result=%q err=%v", result, err)
	}

	var relationship *follows.Follow
	waitFor(t, ctx, "Misskey Accept(Follow)", func() bool {
		remoteActor, findErr := actorRepo.FindByURI(ctx, remoteActorURI)
		if findErr != nil || remoteActor == nil {
			return false
		}
		relationship, findErr = followRepo.Find(ctx, localActor.ID, remoteActor.ID)
		return findErr == nil && relationship != nil && relationship.Status == follows.StatusAccepted
	})

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

	noteURI := "https://a.test/notes/" + created.CreatedNote.ID
	waitFor(t, ctx, "Create(Note) stored by Rosmarinus", func() bool {
		note, findErr := noteRepo.FindByURI(ctx, noteURI)
		return findErr == nil && note != nil && note.Text == "Hello from latest Misskey federation test"
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

func (m *misskeyClient) call(ctx context.Context, endpoint string, payload map[string]any, result any) {
	m.t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		m.t.Fatalf("marshal Misskey %s request: %v", endpoint, err)
	}
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
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		m.t.Fatalf("Misskey %s status=%d body=%s", endpoint, res.StatusCode, responseBody)
	}
	if result != nil && len(responseBody) > 0 {
		if err := json.Unmarshal(responseBody, result); err != nil {
			m.t.Fatalf("decode Misskey %s response: %v body=%s", endpoint, err, responseBody)
		}
	}
}

func waitFor(t *testing.T, ctx context.Context, description string, ready func() bool) {
	t.Helper()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ready() {
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
