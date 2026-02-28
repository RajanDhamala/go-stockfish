package stockfish

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrQueueFull     = errors.New("stockfish queue is full")
	ErrClientClosed  = errors.New("stockfish client is closed")
	ErrEngineStopped = errors.New("stockfish engine is not running")
)

type Config struct {
	BinaryPath               string
	PoolSize                 int
	QueueSize                int
	PerEngineThreads         int
	TotalHashMB              int
	PerEngineHashMB          int
	MaxMultiPV               int
	JobTimeout               time.Duration
	StartTimeout             time.Duration
	ShutdownTimeout          time.Duration
	MaxRestarts              int
	AllowUnsafeCPUOvercommit bool
}

type EvalRequest struct {
	FEN      string
	MoveTime time.Duration
	Depth    int
	MultiPV  int
}

type EvalLine struct {
	MultiPV int
	PV      []string
	Depth   int
	ScoreCP *int
	Mate    *int
}

type EvalResult struct {
	BestMove string
	Ponder   string
	PV       []string
	Depth    int
	ScoreCP  *int
	Mate     *int
	Lines    []EvalLine
}

type OpError struct {
	Op  string
	Err error
}

func (e *OpError) Error() string {
	return fmt.Sprintf("stockfish %s: %v", e.Op, e.Err)
}

func (e *OpError) Unwrap() error {
	return e.Err
}
