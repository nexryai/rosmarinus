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
	apwebfinger "github.com/nexryai/rosmarinus/internal/activitypub/webfinger"
	apworker "github.com/nexryai/rosmarinus/internal/activitypub/worker"
	"github.com/nexryai/rosmarinus/internal/cache"
	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/connector"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	httpserver "github.com/nexryai/rosmarinus/internal/http"
	instancemetadata "github.com/nexryai/rosmarinus/internal/instance"
	"github.com/nexryai/rosmarinus/internal/queue"
	mongostore "github.com/nexryai/rosmarinus/internal/store/mongo"
)

type App struct {
	cfg    config.Config
	logger *log.Logger

	mongoClient                 *mongo.Client
	mongoDB                     *mongo.Database
	redisClient                 *redis.Client
	apLocker                    *cache.Locker
	actors                      *cache.CachedActorRepository
	notes                       *mongostore.NoteRepository
	follows                     *mongostore.FollowRepository
	blocks                      *mongostore.BlockRepository
	reactions                   *mongostore.ReactionRepository
	emojis                      *mongostore.EmojiRepository
	polls                       *mongostore.PollRepository
	media                       *mongostore.MediaRepository
	instances                   *cache.CachedInstanceRepository
	accountCleanup              *mongostore.AccountCleanupRepository
	reports                     *mongostore.ReportRepository
	notifications               *mongostore.NotificationRepository
	salviaAccounts              *mongostore.SalviaAccountRepository
	connectorReceipts           *mongostore.ConnectorReceiptRepository
	localActor                  *actors.Actor
	apClient                    *apclient.Client
	apWorker                    *apworker.Handler
	connectorPublisher          *connector.Publisher
	connectorCommandSource      *connector.AblyCommandSource
	connectorCommands           *connector.CommandHandler
	connectorUnsubscribe        func()
	connectorControlSource      *connector.AblyCommandSource
	connectorControl            *connector.AccountControlHandler
	connectorControlUnsubscribe func()
	connectorAccountReconciler  *connector.AccountReconciler
	queueClient                 *queue.AsynqClient
	queueServer                 *queue.AsynqServer
	httpServer                  *http.Server
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
	salviaAccountRepo := mongostore.NewSalviaAccountRepository(mongoDB, cfg.SalviaAccountCollection)
	connectorReceiptRepo := mongostore.NewConnectorReceiptRepository(mongoDB)
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
	apWorker.SetEmojiRepository(emojiRepo)
	apWorker.SetPollRepository(pollRepo)
	apWorker.SetInstanceRepository(cachedInstanceRepo, instancemetadata.New(cfg.InstanceMetadataTimeout, cfg.UserAgent, cfg.MediaAllowedPrivateNetworks, nil))
	apWorker.SetWebFingerResolver(cache.NewCachedWebFinger(apwebfinger.New(nil, cfg.UserAgent), valueCache))
	apWorker.SetAccountCleanupRepository(accountCleanupRepo)
	apWorker.SetNotificationRepository(notificationRepo)
	var connectorPublisher *connector.Publisher
	var connectorCommandSource *connector.AblyCommandSource
	var connectorCommands *connector.CommandHandler
	var connectorControlSource *connector.AblyCommandSource
	var connectorControl *connector.AccountControlHandler
	var connectorAccountReconciler *connector.AccountReconciler
	if key := cfg.AccountEventPublishAPIKey(); key != "" {
		connectorPublisher, err = connector.NewAblyPublisher(key, cfg.ConnectorAccountEventNamespace)
		if err != nil {
			_ = mongoClient.Disconnect(context.Background())
			_ = redisClient.Close()
			_ = queueClient.Close()
			return nil, fmt.Errorf("create ably connector publisher: %w", err)
		}
		logger.Printf("connector: ably account publisher ready namespace=%s", cfg.ConnectorAccountEventNamespace)
	}
	if key := cfg.CommandSubscribeAPIKey(); key != "" {
		connectorCommandSource, err = connector.NewAblyCommandSource(key, cfg.ConnectorCommandChannel)
		if err != nil {
			_ = mongoClient.Disconnect(context.Background())
			_ = redisClient.Close()
			_ = queueClient.Close()
			return nil, fmt.Errorf("create ably connector command source: %w", err)
		}
		connectorCommands = connector.NewCommandHandler(connectorCommandSource, salviaAccountRepo, cachedActorRepo, apWorker, connectorPublisher, connectorReceiptRepo, logger, cfg.ConnectorReceiptTTL)
	}
	if key := cfg.AccountControlSubscribeAPIKey(); key != "" {
		connectorControlSource, err = connector.NewAblyCommandSource(key, cfg.ConnectorAccountControlChannel)
		if err != nil {
			_ = mongoClient.Disconnect(context.Background())
			_ = redisClient.Close()
			_ = queueClient.Close()
			if connectorCommandSource != nil {
				connectorCommandSource.Close()
			}
			return nil, fmt.Errorf("create ably connector account control source: %w", err)
		}
		connectorControl = connector.NewAccountControlHandler(connectorControlSource, salviaAccountRepo, cachedActorRepo, logger)
	}
	if connectorCommandSource != nil || connectorControlSource != nil {
		connectorAccountReconciler = connector.NewAccountReconciler(salviaAccountRepo, cachedActorRepo, cachedActorRepo, logger)
	}
	if connectorPublisher != nil {
		apWorker.SetConnectorPublisher(connectorPublisher)
	}

	return &App{
		cfg:                        cfg,
		logger:                     logger,
		mongoClient:                mongoClient,
		mongoDB:                    mongoDB,
		redisClient:                redisClient,
		apLocker:                   apLocker,
		actors:                     cachedActorRepo,
		notes:                      noteRepo,
		follows:                    followRepo,
		blocks:                     blockRepo,
		reactions:                  reactionRepo,
		emojis:                     emojiRepo,
		polls:                      pollRepo,
		media:                      mediaRepo,
		instances:                  cachedInstanceRepo,
		accountCleanup:             accountCleanupRepo,
		reports:                    reportRepo,
		notifications:              notificationRepo,
		salviaAccounts:             salviaAccountRepo,
		connectorReceipts:          connectorReceiptRepo,
		localActor:                 localActor,
		apClient:                   apClient,
		apWorker:                   apWorker,
		connectorPublisher:         connectorPublisher,
		connectorCommandSource:     connectorCommandSource,
		connectorCommands:          connectorCommands,
		connectorControlSource:     connectorControlSource,
		connectorControl:           connectorControl,
		connectorAccountReconciler: connectorAccountReconciler,
		queueClient:                queueClient,
		queueServer:                queueServer,
		httpServer: &http.Server{
			Addr:              cfg.HTTPAddr,
			Handler:           httpserver.NewHandlerWithAllStores(cfg, logger, cachedActorRepo, noteRepo, followRepo, reactionRepo, queueClient, pollRepo, mediaRepo, emojiRepo),
			ReadHeaderTimeout: 10 * time.Second,
		},
	}, nil
}

func (a *App) Start(ctx context.Context) error {
	if a.connectorCommands != nil {
		unsubscribe, err := a.connectorCommands.Subscribe(ctx)
		if err != nil {
			return fmt.Errorf("subscribe connector commands: %w", err)
		}
		a.connectorUnsubscribe = unsubscribe
		a.logger.Printf("connector: command subscription ready channel=%s", a.cfg.ConnectorCommandChannel)
	}
	if a.connectorControl != nil {
		unsubscribe, err := a.connectorControl.Subscribe(ctx)
		if err != nil {
			if a.connectorUnsubscribe != nil {
				a.connectorUnsubscribe()
				a.connectorUnsubscribe = nil
			}
			return fmt.Errorf("subscribe connector account control: %w", err)
		}
		a.connectorControlUnsubscribe = unsubscribe
		a.logger.Printf("connector: account control subscription ready channel=%s", a.cfg.ConnectorAccountControlChannel)
	}
	if a.connectorAccountReconciler != nil {
		go a.connectorAccountReconciler.Run(ctx, a.cfg.ConnectorAccountReconcileInterval)
	}
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
	if a.connectorUnsubscribe != nil {
		a.connectorUnsubscribe()
		a.connectorUnsubscribe = nil
	}
	if a.connectorControlUnsubscribe != nil {
		a.connectorControlUnsubscribe()
		a.connectorControlUnsubscribe = nil
	}
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
	if a.connectorCommandSource != nil {
		a.connectorCommandSource.Close()
	}
	if a.connectorControlSource != nil {
		a.connectorControlSource.Close()
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
