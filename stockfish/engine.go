package stockfish

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Engine struct {
	cfg validatedConfig

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	lines     chan string
	readErr   chan error
	waitDone  chan struct{}
	waitErrMu sync.RWMutex
	waitErr   error

	stopCh chan struct{}

	mu        sync.Mutex
	closeOnce sync.Once
	alive     atomic.Bool
}

func newEngine(ctx context.Context, cfg validatedConfig) (*Engine, error) {
	cmd := exec.Command(cfg.binaryPath)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, &OpError{Op: "stdin pipe", Err: err}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, &OpError{Op: "stdout pipe", Err: err}
	}
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return nil, &OpError{Op: "start process", Err: err}
	}

	engine := &Engine{
		cfg:      cfg,
		cmd:      cmd,
		stdin:    stdin,
		stdout:   stdout,
		lines:    make(chan string, 1024),
		readErr:  make(chan error, 1),
		waitDone: make(chan struct{}),
		stopCh:   make(chan struct{}),
	}
	engine.alive.Store(true)

	go engine.readLoop()
	go func() {
		err := cmd.Wait()
		engine.alive.Store(false)
		if err != nil {
			engine.setWaitErr(&OpError{Op: "wait process", Err: err})
		}
		close(engine.waitDone)
	}()

	if err := engine.bootstrap(ctx); err != nil {
		_ = engine.Close(context.Background())
		return nil, err
	}
	return engine, nil
}

func (e *Engine) bootstrap(ctx context.Context) error {
	if err := e.send("uci"); err != nil {
		return err
	}
	if err := e.waitFor(ctx, func(line string) bool { return line == "uciok" }); err != nil {
		return &OpError{Op: "wait uciok", Err: err}
	}

	if err := e.send(fmt.Sprintf("setoption name Threads value %d", e.cfg.perEngineThreads)); err != nil {
		return err
	}
	if err := e.send(fmt.Sprintf("setoption name Hash value %d", e.cfg.perEngineHashMB)); err != nil {
		return err
	}
	if err := e.send("setoption name Ponder value false"); err != nil {
		return err
	}
	if err := e.send("setoption name MultiPV value 1"); err != nil {
		return err
	}
	if err := e.send("isready"); err != nil {
		return err
	}
	if err := e.waitFor(ctx, func(line string) bool { return line == "readyok" }); err != nil {
		return &OpError{Op: "wait readyok", Err: err}
	}
	return nil
}

func (e *Engine) Evaluate(ctx context.Context, request EvalRequest) (EvalResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.alive.Load() {
		return EvalResult{}, ErrEngineStopped
	}

	fen, err := normalizeFEN(request.FEN)
	if err != nil {
		return EvalResult{}, err
	}

	multiPV := request.MultiPV
	if multiPV <= 0 {
		multiPV = 1
	}
	if multiPV > e.cfg.maxMultiPV {
		return EvalResult{}, fmt.Errorf("multipv %d exceeds configured max %d", multiPV, e.cfg.maxMultiPV)
	}
	if err := e.send("setoption name MultiPV value " + strconv.Itoa(multiPV)); err != nil {
		return EvalResult{}, err
	}
	if err := e.send("isready"); err != nil {
		return EvalResult{}, err
	}
	if err := e.waitFor(ctx, func(line string) bool { return line == "readyok" }); err != nil {
		return EvalResult{}, &OpError{Op: "wait readyok", Err: err}
	}

	if err := e.send("position fen " + fen); err != nil {
		return EvalResult{}, err
	}

	if request.Depth > 0 {
		if err := e.send("go depth " + strconv.Itoa(request.Depth)); err != nil {
			return EvalResult{}, err
		}
	} else {
		moveTime := request.MoveTime
		if moveTime <= 0 {
			moveTime = e.cfg.jobTimeout
		}
		moveMillis := int(moveTime.Milliseconds())
		if moveMillis < 1 {
			moveMillis = 1
		}
		if err := e.send("go movetime " + strconv.Itoa(moveMillis)); err != nil {
			return EvalResult{}, err
		}
	}

	result := EvalResult{}
	linesByPV := make(map[int]EvalLine, multiPV)
	for {
		select {
		case <-ctx.Done():
			if stopErr := e.send("stop"); stopErr != nil {
				e.alive.Store(false)
				return EvalResult{}, stopErr
			}
			drainCtx, drainCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			drainErr := e.waitFor(drainCtx, func(line string) bool {
				return strings.HasPrefix(line, "bestmove ")
			})
			drainCancel()
			if drainErr != nil {
				e.alive.Store(false)
			}
			return EvalResult{}, ctx.Err()
		case err := <-e.readErr:
			e.alive.Store(false)
			if err == nil {
				return EvalResult{}, ErrEngineStopped
			}
			return EvalResult{}, err
		case <-e.waitDone:
			e.alive.Store(false)
			waitErr := e.getWaitErr()
			if waitErr == nil {
				return EvalResult{}, ErrEngineStopped
			}
			return EvalResult{}, waitErr
		case line, ok := <-e.lines:
			if !ok {
				e.alive.Store(false)
				return EvalResult{}, ErrEngineStopped
			}

			if update, matched := parseInfoLine(line); matched {
				lineID := 1
				if update.MultiPV != nil && *update.MultiPV > 0 {
					lineID = *update.MultiPV
				}
				if lineID > multiPV {
					continue
				}
				current := linesByPV[lineID]
				current.MultiPV = lineID
				if update.Depth != nil {
					current.Depth = *update.Depth
				}
				if update.ScoreCP != nil {
					current.ScoreCP = update.ScoreCP
					current.Mate = nil
				}
				if update.Mate != nil {
					current.Mate = update.Mate
					current.ScoreCP = nil
				}
				if len(update.PV) > 0 {
					current.PV = append([]string(nil), update.PV...)
				}
				linesByPV[lineID] = current
				continue
			}

			if strings.HasPrefix(line, "bestmove ") {
				bestMove, ponder, ok := parseBestMoveLine(line)
				if !ok {
					return EvalResult{}, &OpError{Op: "parse bestmove", Err: fmt.Errorf("invalid line: %s", line)}
				}
				result.BestMove = bestMove
				result.Ponder = ponder
				result.Lines = sortedEvalLines(linesByPV)
				populatePrimaryResult(&result)
				return result, nil
			}
		}
	}
}

func (e *Engine) Alive() bool {
	return e.alive.Load()
}

func (e *Engine) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	var closeErr error
	e.closeOnce.Do(func() {
		_ = e.send("quit")
		close(e.stopCh)

		select {
		case <-ctx.Done():
			if e.cmd.Process != nil {
				if killErr := e.cmd.Process.Kill(); killErr != nil {
					closeErr = &OpError{Op: "kill process", Err: killErr}
				}
			}
			<-e.waitDone
		case <-e.waitDone:
		}

		e.alive.Store(false)
		_ = e.stdin.Close()
		_ = e.stdout.Close()
	})
	return closeErr
}

func (e *Engine) send(command string) error {
	if !e.alive.Load() {
		return ErrEngineStopped
	}
	if _, err := io.WriteString(e.stdin, command+"\n"); err != nil {
		e.alive.Store(false)
		return &OpError{Op: "write command", Err: err}
	}
	return nil
}

func (e *Engine) waitFor(ctx context.Context, match func(string) bool) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-e.readErr:
			if err == nil {
				return ErrEngineStopped
			}
			return err
		case <-e.waitDone:
			waitErr := e.getWaitErr()
			if waitErr == nil {
				return ErrEngineStopped
			}
			return waitErr
		case line, ok := <-e.lines:
			if !ok {
				return ErrEngineStopped
			}
			if match(line) {
				return nil
			}
		}
	}
}

func (e *Engine) readLoop() {
	scanner := bufio.NewScanner(e.stdout)
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		select {
		case <-e.stopCh:
			close(e.lines)
			return
		case e.lines <- line:
		}
	}

	if err := scanner.Err(); err != nil {
		select {
		case e.readErr <- &OpError{Op: "read output", Err: err}:
		default:
		}
	} else {
		select {
		case e.readErr <- nil:
		default:
		}
	}
	close(e.lines)
}

func (e *Engine) setWaitErr(err error) {
	e.waitErrMu.Lock()
	defer e.waitErrMu.Unlock()
	e.waitErr = err
}

func (e *Engine) getWaitErr() error {
	e.waitErrMu.RLock()
	defer e.waitErrMu.RUnlock()
	return e.waitErr
}

func sortedEvalLines(linesByPV map[int]EvalLine) []EvalLine {
	lines := make([]EvalLine, 0, len(linesByPV))
	for _, line := range linesByPV {
		lines = append(lines, line)
	}
	sort.Slice(lines, func(i, j int) bool {
		return lines[i].MultiPV < lines[j].MultiPV
	})
	return lines
}

func populatePrimaryResult(result *EvalResult) {
	if len(result.Lines) == 0 {
		return
	}
	primary := result.Lines[0]
	for i := range result.Lines {
		if result.Lines[i].MultiPV == 1 {
			primary = result.Lines[i]
			break
		}
	}
	result.Depth = primary.Depth
	result.ScoreCP = primary.ScoreCP
	result.Mate = primary.Mate
	result.PV = append([]string(nil), primary.PV...)
}
