package worker_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"yaConversationWriter/internal/domain"
	"yaConversationWriter/internal/ports"
	"yaConversationWriter/internal/worker"
)

func TestPoolEnforcesConcurrencyLimit(t *testing.T) {
	processor := newBlockingProcessor(8)
	pool := newPool(t, worker.Config{Count: 2, QueueSize: 8, TaskTimeout: time.Second}, processor)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	for i := 0; i < 6; i++ {
		if err := pool.Enqueue(ctx, task(string(rune('a'+i)))); err != nil {
			t.Fatalf("Enqueue(%d) error = %v", i, err)
		}
	}
	waitSignals(t, processor.started, 2)
	if max := processor.maximum(); max != 2 {
		t.Fatalf("maximum concurrency = %d, want 2", max)
	}
	close(processor.release)
	waitSignals(t, processor.finished, 6)
	shutdownPool(t, pool)
}

func TestPoolReportsQueueOverflow(t *testing.T) {
	processor := newBlockingProcessor(4)
	pool := newPool(t, worker.Config{Count: 1, QueueSize: 1, TaskTimeout: time.Second}, processor)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := pool.Enqueue(ctx, task("active")); err != nil {
		t.Fatalf("enqueue active task: %v", err)
	}
	waitSignals(t, processor.started, 1)
	if err := pool.Enqueue(ctx, task("queued")); err != nil {
		t.Fatalf("enqueue queued task: %v", err)
	}
	if err := pool.Enqueue(ctx, task("overflow")); !errors.Is(err, domain.ErrQueueFull) {
		t.Fatalf("overflow error = %v", err)
	}
	close(processor.release)
	waitSignals(t, processor.finished, 2)
	shutdownPool(t, pool)
}

func TestPoolAppliesTaskTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const taskTimeout = time.Hour
		processor := &contextProcessor{started: make(chan struct{}, 1), results: make(chan error, 1)}
		pool := newPool(t, worker.Config{Count: 1, QueueSize: 1, TaskTimeout: taskTimeout}, processor)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := pool.Start(ctx); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		if err := pool.Enqueue(ctx, task("timeout")); err != nil {
			t.Fatalf("Enqueue() error = %v", err)
		}
		waitSignals(t, processor.started, 1)

		startedAt := time.Now()
		if err := <-processor.results; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("processor context error = %v", err)
		}
		if elapsed := time.Since(startedAt); elapsed != taskTimeout {
			t.Fatalf("task timeout elapsed after %v, want %v", elapsed, taskTimeout)
		}
		shutdownPool(t, pool)
	})
}

func TestPoolShutdownCancelsActiveWorkAndRejectsNewTasks(t *testing.T) {
	processor := &contextProcessor{started: make(chan struct{}, 1), results: make(chan error, 1)}
	pool := newPool(t, worker.Config{Count: 1, QueueSize: 2, TaskTimeout: time.Hour}, processor)
	if err := pool.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := pool.Enqueue(context.Background(), task("active")); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	waitSignals(t, processor.started, 1)
	shutdownPool(t, pool)
	if err := <-processor.results; !errors.Is(err, context.Canceled) {
		t.Fatalf("active task context error = %v", err)
	}
	if err := pool.Enqueue(context.Background(), task("late")); !errors.Is(err, domain.ErrShuttingDown) {
		t.Fatalf("enqueue after shutdown error = %v", err)
	}
}

func newPool(t *testing.T, config worker.Config, processor worker.Processor) *worker.Pool {
	t.Helper()
	pool, err := worker.New(config, processor, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return pool
}

func shutdownPool(t *testing.T, pool *worker.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pool.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func task(id string) ports.ProcessingTask {
	meetingID := domain.MeetingID(id)
	return ports.ProcessingTask{
		UserID:    domain.UserID("user"),
		MeetingID: meetingID,
		Audio:     ports.AudioInput{MeetingID: meetingID, Content: []byte("audio")},
	}
}

func waitSignals(t *testing.T, signals <-chan struct{}, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		select {
		case <-signals:
		case <-time.After(time.Second):
			t.Fatalf("received %d of %d expected signals", i, count)
		}
	}
}

type blockingProcessor struct {
	started  chan struct{}
	finished chan struct{}
	release  chan struct{}

	mu        sync.Mutex
	active    int
	maxActive int
}

func newBlockingProcessor(capacity int) *blockingProcessor {
	return &blockingProcessor{
		started:  make(chan struct{}, capacity),
		finished: make(chan struct{}, capacity),
		release:  make(chan struct{}),
	}
}

func (p *blockingProcessor) Process(ctx context.Context, _ ports.ProcessingTask) error {
	p.mu.Lock()
	p.active++
	if p.active > p.maxActive {
		p.maxActive = p.active
	}
	p.mu.Unlock()
	p.started <- struct{}{}

	var err error
	select {
	case <-p.release:
	case <-ctx.Done():
		err = ctx.Err()
	}

	p.mu.Lock()
	p.active--
	p.mu.Unlock()
	p.finished <- struct{}{}
	return err
}

func (p *blockingProcessor) maximum() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.maxActive
}

type contextProcessor struct {
	started chan struct{}
	results chan error
}

func (p *contextProcessor) Process(ctx context.Context, _ ports.ProcessingTask) error {
	if p.started != nil {
		p.started <- struct{}{}
	}
	<-ctx.Done()
	p.results <- ctx.Err()
	return ctx.Err()
}
