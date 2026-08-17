package mock

import (
	"context"
	"sync"
	"time"

	"yaConversationWriter/internal/ports"
)

var _ ports.SpeechClient = (*Client)(nil)

type Config struct {
	Transcript string
	Delay      time.Duration
	Err        error
}

type Client struct {
	config Config

	mu    sync.Mutex
	calls []ports.AudioInput
}

func New(config Config) *Client {
	return &Client{config: config}
}

func (c *Client) Transcribe(ctx context.Context, input ports.AudioInput) (string, error) {
	c.mu.Lock()
	c.calls = append(c.calls, cloneInput(input))
	c.mu.Unlock()

	if err := wait(ctx, c.config.Delay); err != nil {
		return "", err
	}
	if c.config.Err != nil {
		return "", c.config.Err
	}
	return c.config.Transcript, nil
}

func (c *Client) Calls() []ports.AudioInput {
	c.mu.Lock()
	defer c.mu.Unlock()

	calls := make([]ports.AudioInput, len(c.calls))
	for i, call := range c.calls {
		calls[i] = cloneInput(call)
	}
	return calls
}

func cloneInput(input ports.AudioInput) ports.AudioInput {
	input.Content = append([]byte(nil), input.Content...)
	return input
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
