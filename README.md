# go-stockfish

Production-oriented Go package for communicating with Stockfish using persistent pooled engine workers.

## Usage

```go
client, err := stockfish.New(context.Background(), stockfish.Config{
    PoolSize:         2,
    QueueSize:        16,
    PerEngineThreads: 1,
    TotalHashMB:      128,
})
if err != nil { /* handle */ }
defer client.Close(context.Background())

result, err := client.Evaluate(context.Background(), stockfish.EvalRequest{
    FEN:      "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
    MoveTime: 300 * time.Millisecond,
})
```
