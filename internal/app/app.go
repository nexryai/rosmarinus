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

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"

	apclient "github.com/nexryai/rosmarinus/internal/activitypub/client"
	apwebfinger "github.com/nexryai/rosmarinus/internal/activitypub/webfinger"
	apworker "github.com/nexryai/rosmarinus/internal/activitypub/worker"
	api "github.com/nexryai/rosmarinus/internal/api"
	appauth "github.com/nexryai/rosmarinus/internal/auth"
	"github.com/nexryai/rosmarinus/internal/cache"
	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	httpserver "github.com/nexryai/rosmarinus/internal/http"
	instancemetadata "github.com/nexryai/rosmarinus/internal/instance"
	"github.com/nexryai/rosmarinus/internal/queue"
	"github.com/nexryai/rosmarinus/internal/ratelimit"
	"github.com/nexryai/rosmarinus/internal/realtime"
	mongostore "github.com/nexryai/rosmarinus/internal/store/mongo"
)

type App struct {
	cfg    config.Config
	logger *log.Logger

	mongoClient        *mongo.Client
	mongoDB            *mongo.Database
	redisClient        *redis.Client
	apLocker           *cache.Locker
	actors             *cache.CachedActorRepository
	notes              *mongostore.NoteRepository
	follows            *mongostore.FollowRepository
	blocks             *mongostore.BlockRepository
	reactions          *mongostore.ReactionRepository
	emojis             *mongostore.EmojiRepository
	polls              *mongostore.PollRepository
	media              *mongostore.MediaRepository
	instances          *cache.CachedInstanceRepository
	accountCleanup     *mongostore.AccountCleanupRepository
	reports            *mongostore.ReportRepository
	notifications      *mongostore.NotificationRepository
	accounts           *mongostore.AccountRepository
	sessions           *mongostore.SessionRepository
	webauthnCeremonies *mongostore.WebAuthnCeremonyRepository
	settings           *mongostore.SettingsRepository
	realtime           *realtime.RedisBroker
	sessionManager     *appauth.SessionManager
	passkeys           *appauth.PasskeyService
	idempotency        *mongostore.IdempotencyRepository
	activityReceipts   *mongostore.ActivityReceiptRepository
	localActor         *actors.Actor
	apClient           *apclient.Client
	apWorker           *apworker.Handler
	queueClient        *queue.AsynqClient
	queueServer        *queue.AsynqServer
	httpServer         *http.Server
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
	followRepo := mongostore.NewFollowRepository(mongoDB)
	blockRepo := mongostore.NewBlockRepository(mongoDB)
	reactionRepo := mongostore.NewReactionRepository(mongoDB)
	emojiRepo := mongostore.NewEmojiRepository(mongoDB)
	pollRepo := mongostore.NewPollRepository(mongoDB)
	mediaRepo := mongostore.NewMediaRepository(mongoDB)
	instanceRepo := mongostore.NewInstanceRepository(mongoDB)
	accountCleanupRepo := mongostore.NewAccountCleanupRepository(mongoDB)
	reportRepo := mongostore.NewReportRepository(mongoDB)
	notificationRepo := mongostore.NewNotificationRepository(mongoDB)
	salviaReader := mongostore.NewSalviaReader(mongoDB)
	accountRepo := mongostore.NewAccountRepository(mongoDB)
	sessionRepo := mongostore.NewSessionRepository(mongoDB)
	webauthnCeremonyRepo := mongostore.NewWebAuthnCeremonyRepository(mongoDB)
	settingsRepo := mongostore.NewSettingsRepository(mongoDB)
	idempotencyRepo := mongostore.NewIdempotencyRepository(mongoDB)
	activityReceiptRepo := mongostore.NewActivityReceiptRepository(mongoDB)
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
	valueCache := cache.NewRedisValueStore(redisClient, "rosmarinus:cache")
	realtimeBroker := realtime.NewRedisBroker(redisClient)
	cachedActorRepo := cache.NewCachedActorRepository(actorRepo, valueCache)
	cachedInstanceRepo := cache.NewCachedInstanceRepository(instanceRepo, valueCache)

	redisCfg := queue.RedisConfig{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	}
	queueClient := queue.NewAsynqClient(redisCfg)
	apClient := apclient.New(cfg, nil)
	queueServer := queue.NewAsynqServer(redisCfg, queue.WorkerConfig{
		Concurrency: cfg.InboxQueue.Concurrency + cfg.DeliverQueue.Concurrency + 32,
		Tasks: map[string]queue.TaskControls{
			queue.TaskInbox:   {Concurrency: cfg.InboxQueue.Concurrency, RatePerSecond: cfg.InboxQueue.RatePerSecond},
			queue.TaskDeliver: {Concurrency: cfg.DeliverQueue.Concurrency, RatePerSecond: cfg.DeliverQueue.RatePerSecond},
		},
	}, cfg.WorkerQueues, logger)
	apLocker := cache.NewLocker(cache.NewRedisLockStore(redisClient), "rosmarinus:ap", 5*time.Minute)
	apWorker := apworker.New(cfg, logger, cachedActorRepo, noteRepo, followRepo, blockRepo, reactionRepo, reportRepo, queueClient, apClient, localActor)
	apWorker.SetActivityLocker(apLocker)
	apWorker.SetActivityReceiptRepository(activityReceiptRepo)
	apWorker.SetEmojiRepository(emojiRepo)
	apWorker.SetPollRepository(pollRepo)
	apWorker.SetInstanceRepository(cachedInstanceRepo, instancemetadata.New(cfg.InstanceMetadataTimeout, cfg.UserAgent, cfg.MediaAllowedPrivateNetworks, nil))
	apWorker.SetWebFingerResolver(cache.NewCachedWebFinger(apwebfinger.New(nil, cfg.UserAgent), valueCache))
	apWorker.SetAccountCleanupRepository(accountCleanupRepo)
	apWorker.SetNotificationRepository(notificationRepo)
	// Redis Pub/Sub is the local fan-out transport for browser-safe SSE invalidations.
	apWorker.SetConnectorPublisher(realtime.NewDomainPublisher(realtimeBroker, logger))
	sessionManager := appauth.NewSessionManager(sessionRepo, accountRepo, cfg.SessionCookieName, cfg.SessionTTL, cfg.SessionSecure)
	passkeys, err := appauth.NewPasskeyService(&webauthn.Config{
		RPID:          cfg.WebAuthnRPID,
		RPDisplayName: cfg.WebAuthnRPName,
		RPOrigins:     cfg.WebAuthnOrigins,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationRequired,
		},
	}, accountRepo, webauthnCeremonyRepo, sessionManager, cfg.WebAuthnCeremonyTTL)
	if err != nil {
		_ = mongoClient.Disconnect(context.Background())
		_ = redisClient.Close()
		_ = queueClient.Close()
		return nil, err
	}
	authLimiter := ratelimit.NewRedisLimiter(redisClient)
	authAPI := api.NewAuthHandlerWithRateLimit(passkeys, sessionManager, accountRepo, authLimiter, cfg.AuthRateLimit, cfg.AuthRateWindow, logger)
	applicationAPI := api.NewHandlerComplete(
		sessionManager, cachedActorRepo, apWorker, idempotencyRepo, salviaReader, settingsRepo,
		api.NewInstanceInfo(cfg.WebAuthnRPName, cfg.PublicURL, cfg.UserAgent), realtimeBroker, accountRepo, authAPI, logger, cfg.APIIdempotencyTTL,
	)

	return &App{
		cfg:                cfg,
		logger:             logger,
		mongoClient:        mongoClient,
		mongoDB:            mongoDB,
		redisClient:        redisClient,
		apLocker:           apLocker,
		actors:             cachedActorRepo,
		notes:              noteRepo,
		follows:            followRepo,
		blocks:             blockRepo,
		reactions:          reactionRepo,
		emojis:             emojiRepo,
		polls:              pollRepo,
		media:              mediaRepo,
		instances:          cachedInstanceRepo,
		accountCleanup:     accountCleanupRepo,
		reports:            reportRepo,
		notifications:      notificationRepo,
		accounts:           accountRepo,
		sessions:           sessionRepo,
		webauthnCeremonies: webauthnCeremonyRepo,
		settings:           settingsRepo,
		realtime:           realtimeBroker,
		sessionManager:     sessionManager,
		passkeys:           passkeys,
		idempotency:        idempotencyRepo,
		activityReceipts:   activityReceiptRepo,
		localActor:         localActor,
		apClient:           apClient,
		apWorker:           apWorker,
		queueClient:        queueClient,
		queueServer:        queueServer,
		httpServer: &http.Server{
			Addr:              cfg.HTTPAddr,
			Handler:           httpserver.NewHandlerWithAllStoresAndAPI(cfg, logger, cachedActorRepo, noteRepo, followRepo, reactionRepo, queueClient, pollRepo, mediaRepo, emojiRepo, applicationAPI),
			ReadHeaderTimeout: 10 * time.Second,
		},
	}, nil
}

func (a *App) Start(ctx context.Context) error {
	if a.cfg.RunWorkers {
		a.apWorker.Register(a.queueServer)
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
		IsSystemActor: true,
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
