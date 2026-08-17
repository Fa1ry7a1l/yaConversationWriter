package domain

import "errors"

var (
	ErrInvalidArgument   = errors.New("invalid argument")
	ErrNotFound          = errors.New("not found")
	ErrConflict          = errors.New("conflict")
	ErrInvalidTransition = errors.New("invalid status transition")
	ErrUnsupportedFile   = errors.New("unsupported file")
	ErrNotReady          = errors.New("meeting is not ready")
	ErrQueueFull         = errors.New("processing queue is full")
	ErrShuttingDown      = errors.New("application is shutting down")
	ErrInfrastructure    = errors.New("infrastructure failure")
)
