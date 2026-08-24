package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/hibiken/asynq"
)

type taskInspector interface {
	ListArchivedTasks(string, ...asynq.ListOption) ([]*asynq.TaskInfo, error)
	RunTask(string, string) error
	Close() error
}

type Operations struct {
	inspector taskInspector
}

type FailedTask struct {
	ID           string          `json:"id"`
	Queue        string          `json:"queue"`
	Type         string          `json:"type"`
	Payload      json.RawMessage `json:"payload"`
	Retried      int             `json:"retried"`
	MaxRetry     int             `json:"max_retry"`
	LastError    string          `json:"last_error"`
	LastFailedAt time.Time       `json:"last_failed_at"`
}

func NewOperations(redis RedisConfig) *Operations {
	return &Operations{inspector: asynq.NewInspector(redisOpt(redis))}
}

func (o *Operations) Close() error {
	return o.inspector.Close()
}

func (o *Operations) ListFailed(queueName string, limit int) ([]FailedTask, error) {
	queueName = strings.TrimSpace(queueName)
	if queueName == "" {
		return nil, fmt.Errorf("queue name is required")
	}
	if limit <= 0 || limit > 100 {
		return nil, fmt.Errorf("failed-task limit must be between 1 and 100")
	}
	tasks, err := o.inspector.ListArchivedTasks(queueName, asynq.PageSize(limit))
	if err != nil {
		return nil, err
	}
	result := make([]FailedTask, 0, len(tasks))
	for _, task := range tasks {
		result = append(result, FailedTask{
			ID: task.ID, Queue: task.Queue, Type: task.Type, Payload: json.RawMessage(task.Payload),
			Retried: task.Retried, MaxRetry: task.MaxRetry, LastError: task.LastErr, LastFailedAt: task.LastFailedAt,
		})
	}
	return result, nil
}

func (o *Operations) Promote(queueName, taskID string) error {
	queueName = strings.TrimSpace(queueName)
	if queueName != QueueInbox && queueName != QueueDeliver {
		return fmt.Errorf("only %q and %q tasks can be promoted", QueueInbox, QueueDeliver)
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return fmt.Errorf("task id is required")
	}
	return o.inspector.RunTask(queueName, taskID)
}

func RunOperationsCLI(ctx context.Context, args []string, redis RedisConfig, output io.Writer) error {
	_ = ctx
	if len(args) < 1 {
		return fmt.Errorf("usage: rosmarinus queue <failed|promote> ...")
	}
	operations := NewOperations(redis)
	defer operations.Close()
	switch args[0] {
	case "failed":
		if len(args) < 2 || len(args) > 3 {
			return fmt.Errorf("usage: rosmarinus queue failed <queue> [limit]")
		}
		limit := 30
		if len(args) == 3 {
			parsed, err := strconv.Atoi(args[2])
			if err != nil {
				return fmt.Errorf("invalid failed-task limit %q", args[2])
			}
			limit = parsed
		}
		tasks, err := operations.ListFailed(args[1], limit)
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(tasks)
	case "promote":
		if len(args) != 3 {
			return fmt.Errorf("usage: rosmarinus queue promote <inbox|deliver> <task-id>")
		}
		if err := operations.Promote(args[1], args[2]); err != nil {
			return err
		}
		_, err := fmt.Fprintf(output, "promoted queue=%s task_id=%s\n", args[1], args[2])
		return err
	default:
		return fmt.Errorf("unknown queue operation %q", args[0])
	}
}
