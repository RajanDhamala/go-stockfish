package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"go-stockfish/stockfish"
)

func main() {
	client, err := stockfish.New(context.Background(), stockfish.Config{
		PoolSize:         1,
		QueueSize:        8,
		PerEngineThreads: 1,
		PerEngineHashMB:  32,
		JobTimeout:       3 * time.Second,
	})
	if err != nil {
		log.Fatalf("create stockfish client: %v", err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer closeCancel()
		if closeErr := client.Close(closeCtx); closeErr != nil {
			log.Printf("close client: %v", closeErr)
		}
	}()

	reqCtx, reqCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer reqCancel()
	result, err := client.Evaluate(reqCtx, stockfish.EvalRequest{
		FEN:      "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
		MoveTime: 300 * time.Millisecond,
	})
	if err != nil {
		log.Fatalf("evaluate position: %v", err)
	}

	fmt.Printf("bestmove=%s ponder=%s depth=%d pv=%v\n", result.BestMove, result.Ponder, result.Depth, result.PV)
}
