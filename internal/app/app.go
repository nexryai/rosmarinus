package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"

	"github.com/nexryai/rosmarinus/internal/config"
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

	return &App{
		cfg:         cfg,
		logger:      logger,
		mongoClient: mongoClient,
		mongoDB:     mongoDB,
		redisClient: redisClient,
		queueClient: queue.NewAsynqClient(redisCfg),
		queueServer: queue.NewAsynqServer(redisCfg, 10, cfg.WorkerQueues, logger),
		httpServer: &http.Server{
			Addr:              cfg.HTTPAddr,
			Handler:           httpserver.NewHandler(logger),
			ReadHeaderTimeout: 10 * time.Second,
		},
	}, nil
}

func (a *App) Start(ctx context.Context) error {
	_ = ctx
	if a.cfg.RunWorkers {
		a.queueServer.RegisterNoopHandlers()
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
