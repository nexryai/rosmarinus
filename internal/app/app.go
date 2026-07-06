package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"

	apclient "github.com/nexryai/rosmarinus/internal/activitypub/client"
	apworker "github.com/nexryai/rosmarinus/internal/activitypub/worker"
	"github.com/nexryai/rosmarinus/internal/cache"
	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	httpserver "github.com/nexryai/rosmarinus/internal/http"
	"github.com/nexryai/rosmarinus/internal/queue"
	mongostore "github.com/nexryai/rosmarinus/internal/store/mongo"
)

type App struct {
	cfg    config.Config
	logger *log.Logger

	mongoClient *mongo.Client
	mongoDB     *mongo.Database
	redisClient *redis.Client
	apLocker    *cache.Locker
	actors      *mongostore.ActorRepository
	notes       *mongostore.NoteRepository
	localActor  *actors.Actor
	apClient    *apclient.Client
	apWorker    *apworker.Handler
	queueClient *queue.AsynqClient
	queueServer *queue.AsynqServer
	httpServer  *http.Server
}

func Run(ctx context.Context, logger *log.Logger) error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}
	app, err := New(ctx, cfg, logger)
	if err != nil {
		return err
	}
	runCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Start(runCtx); err != nil {
		_ = app.Shutdown(context.Background())
		return err
	}
	<-runCtx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return app.Shutdown(shutdownCtx)
}

func New(ctx context.Context, cfg config.Config, logger *log.Logger) (*App, error) {
	if logger == nil {
		logger = log.New(os.Stdout, "", log.LstdFlags)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	mongoClient, err := mongo.Connect(options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		return nil, fmt.Errorf("connect mongodb: %w", err)
	}
	if err := mongoClient.Ping(ctx, readpref.Primary()); err != nil {
		_ = mongoClient.Disconnect(context.Background())
		return nil, fmt.Errorf("ping mongodb: %w", err)
	}
	mongoDB := mongoClient.Database(cfg.MongoDatabase)
	if err := mongostore.BootstrapIndexes(ctx, mongoDB); err != nil {
		_ = mongoClient.Disconnect(context.Background())
		return nil, fmt.Errorf("bootstrap mongodb indexes: %w", err)
	}
	actorRepo := mongostore.NewActorRepository(mongoDB)
	noteRepo := mongostore.NewNoteRepository(mongoDB)
	var localActor *actors.Actor
	if cfg.LocalActorUsername != "" {
		localActor, err = actorRepo.EnsureLocalActor(ctx, localActorFromConfig(cfg))
		if err != nil {
			_ = mongoClient.Disconnect(context.Background())
			return nil, fmt.Errorf("bootstrap local actor: %w", err)
		}
		logger.Printf("activitypub: local actor ready username=%s", cfg.LocalActorUsername)
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		_ = mongoClient.Disconnect(context.Background())
		_ = redisClient.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	redisCfg := queue.RedisConfig{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	}
	queueClient := queue.NewAsynqClient(redisCfg)
	apClient := apclient.New(cfg, nil)
	queueServer := queue.NewAsynqServer(redisCfg, 10, cfg.WorkerQueues, logger)
	apWorker := apworker.New(cfg, logger, actorRepo, noteRepo, queueClient, apClient, localActor)

	return &App{
		cfg:         cfg,
		logger:      logger,
		mongoClient: mongoClient,
		mongoDB:     mongoDB,
		redisClient: redisClient,
		apLocker:    cache.NewLocker(cache.NewRedisLockStore(redisClient), "rosmarinus:ap", 5*time.Minute),
		actors:      actorRepo,
		notes:       noteRepo,
		localActor:  localActor,
		apClient:    apClient,
		apWorker:    apWorker,
		queueClient: queueClient,
		queueServer: queueServer,
		httpServer: &http.Server{
			Addr:              cfg.HTTPAddr,
			Handler:           httpserver.NewHandlerWithStores(cfg, logger, actorRepo, noteRepo, queueClient),
			ReadHeaderTimeout: 10 * time.Second,
		},
	}, nil
}

func (a *App) Start(ctx context.Context) error {
	_ = ctx
	if a.cfg.RunWorkers {
		a.apWorker.Register(a.queueServer)
		a.queueServer.RegisterSystemNoopHandlers()
		go func() {
			if err := a.queueServer.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				a.logger.Printf("queue worker stopped: %v", err)
			}
		}()
	}
	if a.cfg.RunHTTP {
		go func() {
			a.logger.Printf("http: listening on %s", a.cfg.HTTPAddr)
			if err := a.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				a.logger.Printf("http server stopped: %v", err)
			}
		}()
	}
	return nil
}

func (a *App) Shutdown(ctx context.Context) error {
	var errs []error
	if a.httpServer != nil {
		if err := a.httpServer.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs = append(errs, err)
		}
	}
	if a.queueServer != nil {
		a.queueServer.Shutdown()
	}
	if a.queueClient != nil {
		if err := a.queueClient.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if a.redisClient != nil {
		if err := a.redisClient.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if a.mongoClient != nil {
		if err := a.mongoClient.Disconnect(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func localActorFromConfig(cfg config.Config) actors.Actor {
	base := strings.TrimRight(cfg.PublicURL, "/")
	id := cfg.LocalActorID
	if id == "" {
		id = cfg.LocalActorUsername
	}
	actorURI := base + "/users/" + id
	name := cfg.LocalActorDisplayName
	if name == "" {
		name = cfg.LocalActorUsername
	}
	return actors.Actor{
		ID:            id,
		Username:      cfg.LocalActorUsername,
		UsernameLower: strings.ToLower(cfg.LocalActorUsername),
		Name:          name,
		Type:          cfg.LocalActorType,
		URI:           actorURI,
		Inbox:         actorURI + "/inbox",
		SharedInbox:   base + "/inbox",
		PublicKeyID:   actorURI + "#main-key",
		IsSuspended:   false,
	}
}
