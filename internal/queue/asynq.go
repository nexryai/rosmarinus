package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/hibiken/asynq"
)

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type AsynqClient struct {
	client *asynq.Client
}

func NewAsynqClient(redis RedisConfig) *AsynqClient {
	return &AsynqClient{
		client: asynq.NewClient(redisOpt(redis)),
	}
}

func (c *AsynqClient) Enqueue(ctx context.Context, task Task) error {
	payload, err := json.Marshal(task.Payload)
	if err != nil {
		return err
	}
	options := []asynq.Option{
		asynq.Queue(task.Queue),
		asynq.MaxRetry(task.MaxRetry),
	}
	if task.Timeout > 0 {
		options = append(options, asynq.Timeout(task.Timeout))
	}
	if task.ProcessIn > 0 {
		options = append(options, asynq.ProcessIn(task.ProcessIn))
	}
	_, err = c.client.EnqueueContext(ctx, asynq.NewTask(task.Type, payload), options...)
	return err
}

func (c *AsynqClient) Close() error {
	return c.client.Close()
}

type AsynqServer struct {
	server *asynq.Server
	mux    *asynq.ServeMux
	logger *log.Logger
}

func NewAsynqServer(redis RedisConfig, concurrency int, queues []string, logger *log.Logger) *AsynqServer {
	if concurrency <= 0 {
		concurrency = 10
	}
	queueWeights := make(map[string]int, len(queues))
	for _, queue := range queues {
		if queue != "" {
			queueWeights[queue] = 1
		}
	}
	if len(queueWeights) == 0 {
		queueWeights[QueueInbox] = 1
		queueWeights[QueueDeliver] = 1
	}
	return &AsynqServer{
		server: asynq.NewServer(redisOpt(redis), asynq.Config{
			Concurrency:    concurrency,
			Queues:         queueWeights,
			RetryDelayFunc: APBackoff,
		}),
		mux:    asynq.NewServeMux(),
		logger: logger,
	}
}

func (s *AsynqServer) HandleFunc(pattern string, handler asynq.HandlerFunc) {
	s.mux.HandleFunc(pattern, handler)
}

func (s *AsynqServer) RegisterNoopHandlers() {
	for _, typ := range []string{TaskInbox, TaskDeliver, TaskMetadata, TaskMedia, TaskPollEnded, TaskAccountDelete} {
		taskType := typ
		s.HandleFunc(taskType, func(ctx context.Context, task *asynq.Task) error {
			if s.logger != nil {
				s.logger.Printf("queue: no-op handler processed task type=%s", task.Type())
			}
			return nil
		})
	}
}

func (s *AsynqServer) Start() error {
	if s.logger != nil {
		s.logger.Printf("queue: starting asynq worker")
	}
	if err := s.server.Start(s.mux); err != nil {
		return fmt.Errorf("start asynq server: %w", err)
	}
	return nil
}

func (s *AsynqServer) Shutdown() {
	if s.logger != nil {
		s.logger.Printf("queue: shutting down asynq worker")
	}
	s.server.Shutdown()
}

func APBackoff(attemptsMade int, err error, task *asynq.Task) time.Duration {
	backoff := apBackoffBase(attemptsMade)
	jitter := time.Duration(rand.Float64() * 0.2 * float64(backoff))
	return backoff + jitter
}

func apBackoffBase(retriesCompleted int) time.Duration {
	const baseDelay = time.Minute
	const maxBackoff = 8 * time.Hour

	// BullMQ passes one-based attemptsMade to Misskey's strategy. Asynq passes
	// the number of retries already completed, which is zero on the first
	// failure, so advance it once to preserve the wire-retry schedule.
	attemptsMade := retriesCompleted + 1
	if attemptsMade < 1 {
		attemptsMade = 1
	}
	if attemptsMade >= 9 {
		return maxBackoff
	}
	backoff := time.Duration((1<<attemptsMade)-1) * baseDelay
	if backoff > maxBackoff {
		backoff = maxBackoff
	}
	return backoff
}

func redisOpt(redis RedisConfig) asynq.RedisClientOpt {
	return asynq.RedisClientOpt{
		Addr:     redis.Addr,
		Password: redis.Password,
		DB:       redis.DB,
	}
}
