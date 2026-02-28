# go-stockfish

Production-oriented Go package for communicating with Stockfish using persistent pooled engine workers.

## Usage

```go
client, err := stockfish.New(context.Background(), stockfish.Config{
    PoolSize:         2,
    QueueSize:        16,
    PerEngineThreads: 1,
    TotalHashMB:      128,
    MaxMultiPV:       5,
})
if err != nil { /* handle */ }
defer client.Close(context.Background())

result, err := client.Evaluate(context.Background(), stockfish.EvalRequest{
    FEN:      "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
    MoveTime: 300 * time.Millisecond,
    MultiPV:  3,
})
if err != nil { /* handle */ }

fmt.Println(result.BestMove) // top line (MultiPV=1)
for _, line := range result.Lines {
    fmt.Printf("line=%d depth=%d pv=%v cp=%v mate=%v\n", line.MultiPV, line.Depth, line.PV, line.ScoreCP, line.Mate)
}
```

## Security and reliability hardening

- FEN input is normalized and validated as single-line data to prevent command-injection via newline-separated UCI commands.
- `MaxMultiPV` bounds per-request line count (`EvalRequest.MultiPV`) to prevent resource abuse.
- Engine process wait state is synchronized to avoid races in concurrent shutdown/error paths.

## Architecture

1. `Client` owns a bounded job queue and a pool of long-lived `Engine` workers.
2. Each worker keeps one Stockfish subprocess alive and evaluates jobs sequentially over UCI.
3. `Evaluate` enqueues requests with context deadlines; workers send UCI commands and parse `info`/`bestmove` output.
4. `EvalResult` exposes both the primary line (`PV`, `ScoreCP`/`Mate`) and full multi-line analysis (`Lines`).
5. On engine failure, workers restart engines with bounded retries (`MaxRestarts`) and continue serving queued requests.
