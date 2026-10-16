package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/taskruntime"
)

type stubTaskSession struct {
	ctx  context.Context
	task *model.Task
}

func (s stubTaskSession) Context() context.Context { return s.ctx }
func (s stubTaskSession) Task() *model.Task        { return s.task }
func (s stubTaskSession) Lost() bool               { return false }
func (s stubTaskSession) LostErr() error           { return nil }

func TestTaskUserIdentityRequiresOwner(t *testing.T) {
	for _, task := range []*model.Task{nil, {ID: "task-no-user"}, {ID: "task-blank", UserID: "   "}} {
		if _, err := taskUserIdentity(task); err == nil {
			t.Fatalf("task %#v accepted without a user identity", task)
		}
	}
	userID, err := taskUserIdentity(&model.Task{ID: "task-owner", UserID: " u_alice "})
	if err != nil || userID != "u_alice" {
		t.Fatalf("userID=%q err=%v", userID, err)
	}
}

// OrcaTV 没有共享 owner workspace：领取到的任务若没有 user_id，必须终态拒绝，
// 不能落到统一 owner 目录下执行。
func TestClaimedTaskWithoutOwnerIsRefusedBeforeExecution(t *testing.T) {
	service, db := retiredAgentService(t)
	leaseExpiresAt := time.Now().Add(time.Minute)
	task := &model.Task{
		ID: "task-no-owner", Type: "canvas_text", Status: model.TaskStatusRunning,
		LeaseOwner: "lease-1", LeaseExpiresAt: &leaseExpiresAt,
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	outcome := service.taskWorker().executeClaimed(stubTaskSession{ctx: context.Background(), task: task})
	if outcome.Kind != taskruntime.KindRejected {
		t.Fatalf("kind=%s outcome=%+v", outcome.Kind, outcome)
	}
	if !outcome.Applied {
		t.Fatalf("task without owner was not terminally settled: %+v", outcome)
	}
	var stored model.Task
	if err := db.First(&stored, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.TaskStatusFailed {
		t.Fatalf("status=%s", stored.Status)
	}
	if stored.Stage != "身份缺失" {
		t.Fatalf("stage=%q", stored.Stage)
	}
	if !strings.Contains(stored.Error, "用户身份") {
		t.Fatalf("error=%q", stored.Error)
	}
}
