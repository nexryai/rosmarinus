package queue

import (
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

type fakeInspector struct {
	tasks     []*asynq.TaskInfo
	listQueue string
	runQueue  string
	runTaskID string
	closed    bool
}

func (i *fakeInspector) ListArchivedTasks(queueName string, _ ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	i.listQueue = queueName
	return i.tasks, nil
}

func (i *fakeInspector) RunTask(queueName, taskID string) error {
	i.runQueue = queueName
	i.runTaskID = taskID
	return nil
}

func (i *fakeInspector) Close() error {
	i.closed = true
	return nil
}

func TestOperationsListFailed(t *testing.T) {
	failedAt := time.Now().UTC()
	inspector := &fakeInspector{tasks: []*asynq.TaskInfo{{
		ID: "task-id", Queue: QueueDeliver, Type: TaskDeliver,
		Payload: []byte(`{"version":1}`), Retried: 11, MaxRetry: 11,
		LastErr: "delivery failed", LastFailedAt: failedAt,
	}}}
	operations := &Operations{inspector: inspector}
	tasks, err := operations.ListFailed(QueueDeliver, 10)
	if err != nil {
		t.Fatalf("ListFailed returned error: %v", err)
	}
	if inspector.listQueue != QueueDeliver || len(tasks) != 1 || tasks[0].ID != "task-id" || tasks[0].LastError != "delivery failed" || string(tasks[0].Payload) != `{"version":1}` {
		t.Fatalf("unexpected failed tasks: %+v", tasks)
	}
	if _, err := operations.ListFailed(QueueDeliver, 0); err == nil {
		t.Fatal("zero limit was accepted")
	}
}

func TestOperationsPromoteOnlyFederationQueues(t *testing.T) {
	inspector := &fakeInspector{}
	operations := &Operations{inspector: inspector}
	if err := operations.Promote(QueueInbox, "task-id"); err != nil {
		t.Fatalf("Promote returned error: %v", err)
	}
	if inspector.runQueue != QueueInbox || inspector.runTaskID != "task-id" {
		t.Fatalf("unexpected promoted task: queue=%q id=%q", inspector.runQueue, inspector.runTaskID)
	}
	if err := operations.Promote(QueueMedia, "task-id"); err == nil {
		t.Fatal("non-federation queue promotion was accepted")
	}
}
