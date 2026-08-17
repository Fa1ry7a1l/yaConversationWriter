package mock

import (
	"context"
	"sync"
	"time"

	"yaConversationWriter/internal/ports"
)

var _ ports.LLMClient = (*Client)(nil)

type Config struct {
	Summary      string
	Answer       string
	SummaryDelay time.Duration
	AnswerDelay  time.Duration
	SummaryErr   error
	AnswerErr    error
}

type Client struct {
	config Config

	mu           sync.Mutex
	summaryCalls []ports.SummaryRequest
	answerCalls  []ports.AnswerRequest
}

func New(config Config) *Client {
	return &Client{config: config}
}

func (c *Client) Summarize(ctx context.Context, request ports.SummaryRequest) (string, error) {
	c.mu.Lock()
	c.summaryCalls = append(c.summaryCalls, request)
	c.mu.Unlock()

	if err := wait(ctx, c.config.SummaryDelay); err != nil {
		return "", err
	}
	if c.config.SummaryErr != nil {
		return "", c.config.SummaryErr
	}
	return c.config.Summary, nil
}

func (c *Client) Answer(ctx context.Context, request ports.AnswerRequest) (string, error) {
	c.mu.Lock()
	c.answerCalls = append(c.answerCalls, request)
	c.mu.Unlock()

	if err := wait(ctx, c.config.AnswerDelay); err != nil {
		return "", err
	}
	if c.config.AnswerErr != nil {
		return "", c.config.AnswerErr
	}
	return c.config.Answer, nil
}

func (c *Client) SummaryCalls() []ports.SummaryRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ports.SummaryRequest(nil), c.summaryCalls...)
}

func (c *Client) AnswerCalls() []ports.AnswerRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ports.AnswerRequest(nil), c.answerCalls...)
}

func wait(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if delay == 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
