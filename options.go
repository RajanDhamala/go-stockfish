package stockfish

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	defaultQueueMultiplier = 4
	defaultHashMB          = 16
	defaultMaxMultiPV      = 5
	maxAllowedMultiPV      = 256
	defaultJobTimeout      = 5 * time.Second
	defaultStartTimeout    = 5 * time.Second
	defaultShutdownTimeout = 5 * time.Second
	defaultMaxRestarts     = 1
)

type validatedConfig struct {
	binaryPath       string
	poolSize         int
	queueSize        int
	perEngineThreads int
	perEngineHashMB  int
	maxMultiPV       int
	jobTimeout       time.Duration
	startTimeout     time.Duration
	shutdownTimeout  time.Duration
	maxRestarts      int
}

type Client struct {
	cfg validatedConfig

	jobs chan evalJob

	ctx    context.Context
	cancel context.CancelFunc

	wg        sync.WaitGroup
	closeOnce sync.Once
}

type evalJob struct {
	ctx      context.Context
	request  EvalRequest
	response chan evalResponse
}

type evalResponse struct {
	result EvalResult
	err    error
}

func New(ctx context.Context, cfg Config) (*Client, error) {
	normalized, err := validateConfig(cfg)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}

	clientCtx, cancel := context.WithCancel(ctx)
	c := &Client{
		cfg:    normalized,
		jobs:   make(chan evalJob, normalized.queueSize),
		ctx:    clientCtx,
		cancel: cancel,
	}

	engines := make([]*Engine, 0, normalized.poolSize)
	for i := 0; i < normalized.poolSize; i++ {
		engineCtx, engineCancel := context.WithTimeout(clientCtx, normalized.startTimeout)
		engine, startErr := newEngine(engineCtx, normalized)
		engineCancel()
		if startErr != nil {
			cancel()
			close(c.jobs)
			for _, startedEngine := range engines {
				_ = closeEngine(startedEngine, normalized.shutdownTimeout)
			}
			return nil, startErr
		}
		engines = append(engines, engine)
	}

	for i := range engines {
		c.wg.Add(1)
		go c.worker(engines[i])
	}

	return c, nil
}

func (c *Client) Evaluate(ctx context.Context, request EvalRequest) (EvalResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	fen, err := normalizeFEN(request.FEN)
	if err != nil {
		return EvalResult{}, err
	}
	request.FEN = fen

	if request.MultiPV < 0 {
		return EvalResult{}, fmt.Errorf("multipv must be >= 0")
	}
	if request.MultiPV == 0 {
		request.MultiPV = 1
	}
	if request.MultiPV > c.cfg.maxMultiPV {
		return EvalResult{}, fmt.Errorf("multipv %d exceeds configured max %d", request.MultiPV, c.cfg.maxMultiPV)
	}

	evalCtx := ctx
	cancel := func() {}
	if c.cfg.jobTimeout > 0 {
		evalCtx, cancel = context.WithTimeout(ctx, c.cfg.jobTimeout)
	}
	defer cancel()

	job := evalJob{
		ctx:      evalCtx,
		request:  request,
		response: make(chan evalResponse, 1),
	}

	select {
	case <-c.ctx.Done():
		return EvalResult{}, ErrClientClosed
	case c.jobs <- job:
	default:
		return EvalResult{}, ErrQueueFull
	}

	select {
	case response := <-job.response:
		return response.result, response.err
	case <-evalCtx.Done():
		return EvalResult{}, evalCtx.Err()
	case <-c.ctx.Done():
		return EvalResult{}, ErrClientClosed
	}
}

func (c *Client) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	var closeErr error
	c.closeOnce.Do(func() {
		c.cancel()
		close(c.jobs)

		done := make(chan struct{})
		go func() {
			c.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-ctx.Done():
			closeErr = ctx.Err()
		}
	})
	return closeErr
}

func (c *Client) worker(engine *Engine) {
	defer c.wg.Done()
	defer func() {
		_ = closeEngine(engine, c.cfg.shutdownTimeout)
	}()

	for {
		select {
		case <-c.ctx.Done():
			return
		case job, ok := <-c.jobs:
			if !ok {
				return
			}

			result, err := engine.Evaluate(job.ctx, job.request)
			if shouldRestartEngine(err, engine) {
				_ = closeEngine(engine, c.cfg.shutdownTimeout)
				restartedEngine, restartErr := c.startEngineWithRetries()
				if restartErr != nil {
					if err == nil {
						err = restartErr
					} else {
						err = fmt.Errorf("%w: %v", err, restartErr)
					}
				} else {
					engine = restartedEngine
					if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
						result, err = engine.Evaluate(job.ctx, job.request)
					}
				}
			}

			job.response <- evalResponse{result: result, err: err}
		}
	}
}

func (c *Client) startEngineWithRetries() (*Engine, error) {
	var lastErr error
	for attempt := 0; attempt <= c.cfg.maxRestarts; attempt++ {
		if c.ctx.Err() != nil {
			return nil, ErrClientClosed
		}
		startCtx, startCancel := context.WithTimeout(c.ctx, c.cfg.startTimeout)
		engine, err := newEngine(startCtx, c.cfg)
		startCancel()
		if err == nil {
			return engine, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("engine restart failed after %d attempts: %w", c.cfg.maxRestarts+1, lastErr)
}

func shouldRestartEngine(err error, engine *Engine) bool {
	if engine == nil {
		return true
	}
	if !engine.Alive() {
		return true
	}
	return errors.Is(err, ErrEngineStopped)
}

func validateConfig(cfg Config) (validatedConfig, error) {
	cpuCount := runtime.NumCPU()
	if cpuCount < 1 {
		cpuCount = 1
	}

	poolSize := cfg.PoolSize
	if poolSize <= 0 {
		poolSize = 1
	}
	if poolSize < 1 {
		return validatedConfig{}, fmt.Errorf("pool size must be >= 1")
	}
	if !cfg.AllowUnsafeCPUOvercommit && poolSize > cpuCount {
		return validatedConfig{}, fmt.Errorf("pool size %d exceeds CPU count %d", poolSize, cpuCount)
	}

	perEngineThreads := cfg.PerEngineThreads
	if perEngineThreads <= 0 {
		perEngineThreads = 1
	}
	if perEngineThreads < 1 {
		return validatedConfig{}, fmt.Errorf("per-engine threads must be >= 1")
	}
	if !cfg.AllowUnsafeCPUOvercommit && poolSize*perEngineThreads > cpuCount {
		return validatedConfig{}, fmt.Errorf("pool_size*per_engine_threads exceeds CPU count (%d > %d)", poolSize*perEngineThreads, cpuCount)
	}

	queueSize := cfg.QueueSize
	if queueSize <= 0 {
		queueSize = poolSize * defaultQueueMultiplier
	}
	if queueSize < 1 {
		return validatedConfig{}, fmt.Errorf("queue size must be >= 1")
	}

	perEngineHashMB := cfg.PerEngineHashMB
	if cfg.TotalHashMB > 0 {
		perEngineHashMB = cfg.TotalHashMB / poolSize
	}
	if perEngineHashMB <= 0 {
		perEngineHashMB = defaultHashMB
	}
	if perEngineHashMB < 1 {
		return validatedConfig{}, fmt.Errorf("per-engine hash must be >= 1 MB")
	}

	maxRestarts := cfg.MaxRestarts
	if maxRestarts < 0 {
		return validatedConfig{}, fmt.Errorf("max restarts must be >= 0")
	}
	if maxRestarts == 0 {
		maxRestarts = defaultMaxRestarts
	}

	maxMultiPV := cfg.MaxMultiPV
	if maxMultiPV < 0 {
		return validatedConfig{}, fmt.Errorf("max multipv must be >= 0")
	}
	if maxMultiPV == 0 {
		maxMultiPV = defaultMaxMultiPV
	}
	if maxMultiPV > maxAllowedMultiPV {
		return validatedConfig{}, fmt.Errorf("max multipv must be <= %d", maxAllowedMultiPV)
	}

	jobTimeout := cfg.JobTimeout
	if jobTimeout <= 0 {
		jobTimeout = defaultJobTimeout
	}

	startTimeout := cfg.StartTimeout
	if startTimeout <= 0 {
		startTimeout = defaultStartTimeout
	}

	shutdownTimeout := cfg.ShutdownTimeout
	if shutdownTimeout <= 0 {
		shutdownTimeout = defaultShutdownTimeout
	}

	binaryPath, err := resolveBinaryPath(cfg.BinaryPath)
	if err != nil {
		return validatedConfig{}, err
	}

	return validatedConfig{
		binaryPath:       binaryPath,
		poolSize:         poolSize,
		queueSize:        queueSize,
		perEngineThreads: perEngineThreads,
		perEngineHashMB:  perEngineHashMB,
		maxMultiPV:       maxMultiPV,
		jobTimeout:       jobTimeout,
		startTimeout:     startTimeout,
		shutdownTimeout:  shutdownTimeout,
		maxRestarts:      maxRestarts,
	}, nil
}

func resolveBinaryPath(configuredPath string) (string, error) {
	trimmed := strings.TrimSpace(configuredPath)
	if trimmed != "" {
		if found, err := exec.LookPath(trimmed); err == nil {
			return found, nil
		}
	}

	if found, err := exec.LookPath("stockfish"); err == nil {
		return found, nil
	}

	if trimmed == "" {
		return "", fmt.Errorf("stockfish binary not found in PATH")
	}
	return "", fmt.Errorf("stockfish binary not found at %q and default lookup failed", trimmed)
}

func closeEngine(engine *Engine, timeout time.Duration) error {
	if engine == nil {
		return nil
	}
	closeCtx, closeCancel := context.WithTimeout(context.Background(), timeout)
	defer closeCancel()
	return engine.Close(closeCtx)
}

func normalizeFEN(fen string) (string, error) {
	trimmed := strings.TrimSpace(fen)
	if trimmed == "" {
		return "", fmt.Errorf("fen must not be empty")
	}
	if strings.ContainsAny(trimmed, "\r\n") {
		return "", fmt.Errorf("fen must be single-line")
	}
	return trimmed, nil
}
