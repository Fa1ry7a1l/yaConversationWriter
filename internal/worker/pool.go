package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"yaConversationWriter/internal/domain"
	"yaConversationWriter/internal/ports"
)

var _ ports.JobDispatcher = (*Pool)(nil)

type Processor interface {
	Process(ctx context.Context, task ports.ProcessingTask) error
}

type Config struct {
	Count       int
	QueueSize   int
	TaskTimeout time.Duration
}

type Pool struct {
	config    Config
	processor Processor
	logger    *slog.Logger
	queue     chan ports.ProcessingTask

	mu        sync.RWMutex
	started   bool
	accepting bool
	cancel    context.CancelFunc
	runDone   <-chan struct{}
	done      chan struct{}
	waitGroup sync.WaitGroup
}

func New(config Config, processor Processor, logger *slog.Logger) (*Pool, error) {
	if config.Count <= 0 {
		return nil, errors.New("worker count must be greater than zero")
	}
	if config.QueueSize <= 0 {
		return nil, errors.New("worker queue size must be greater than zero")
	}
	if config.TaskTimeout <= 0 {
		return nil, errors.New("worker task timeout must be greater than zero")
	}
	if processor == nil {
		return nil, errors.New("processor is required")
	}
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	return &Pool{
		config:    config,
		processor: processor,
		logger:    logger,
		queue:     make(chan ports.ProcessingTask, config.QueueSize),
	}, nil
}

func (p *Pool) Name() string { return "workers" }

func (p *Pool) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started {
		return fmt.Errorf("%w: worker pool already started", domain.ErrConflict)
	}

	runCtx, cancel := context.WithCancel(ctx)
	p.started = true
	p.accepting = true
	p.cancel = cancel
	p.runDone = runCtx.Done()
	p.done = make(chan struct{})
	p.waitGroup.Add(p.config.Count)
	for id := 1; id <= p.config.Count; id++ {
		go p.runWorker(runCtx, id)
	}
	done := p.done
	go func() {
		p.waitGroup.Wait()
		close(done)
	}()
	p.logger.Info("worker pool started", "workers", p.config.Count, "queue_size", p.config.QueueSize)
	return nil
}

func (p *Pool) Enqueue(ctx context.Context, task ports.ProcessingTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if task.UserID == "" || task.MeetingID == "" || task.Audio.MeetingID != task.MeetingID {
		return fmt.Errorf("%w: invalid processing task", domain.ErrInvalidArgument)
	}

	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.started || !p.accepting {
		return domain.ErrShuttingDown
	}
	select {
	case <-p.runDone:
		return domain.ErrShuttingDown
	default:
	}

	task.Audio.Content = append([]byte(nil), task.Audio.Content...)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.runDone:
		return domain.ErrShuttingDown
	case p.queue <- task:
		p.logger.Info("processing job enqueued", "user_id", task.UserID, "meeting_id", task.MeetingID)
		return nil
	default:
		return domain.ErrQueueFull
	}
}

func (p *Pool) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	if !p.started {
		p.mu.Unlock()
		return nil
	}
	p.accepting = false
	cancel := p.cancel
	done := p.done
	p.mu.Unlock()

	cancel()
	select {
	case <-done:
		p.logger.Info("worker pool stopped")
		return nil
	default:
	}
	select {
	case <-done:
		p.logger.Info("worker pool stopped")
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for worker pool shutdown: %w", ctx.Err())
	}
}

func (p *Pool) runWorker(ctx context.Context, id int) {
	defer p.waitGroup.Done()
	p.logger.Debug("worker started", "worker", id)
	defer p.logger.Debug("worker stopped", "worker", id)

	for {
		select {
		case task := <-p.queue:
			p.process(ctx, id, task)
		case <-ctx.Done():
			p.drain(ctx, id)
			return
		}
	}
}

func (p *Pool) drain(ctx context.Context, id int) {
	for {
		select {
		case task := <-p.queue:
			p.process(ctx, id, task)
		default:
			return
		}
	}
}

func (p *Pool) process(parent context.Context, workerID int, task ports.ProcessingTask) {
	ctx, cancel := context.WithTimeout(parent, p.config.TaskTimeout)
	defer cancel()

	if err := p.processor.Process(ctx, task); err != nil {
		p.logger.Error("processing job failed", "worker", workerID, "user_id", task.UserID, "meeting_id", task.MeetingID, "error", err)
		return
	}
	p.logger.Info("processing job completed", "worker", workerID, "user_id", task.UserID, "meeting_id", task.MeetingID)
}
