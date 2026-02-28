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
		MaxMultiPV:       5,
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
		FEN:      "r1bq1bkr/ppp3pp/2n5/3np3/2B5/5Q2/PPPP1PPP/RNB1K2R w KQ - 2 8",
		MoveTime: 300 * time.Millisecond,
		// Depth:   16,
		MultiPV: 5,
	})
	if err != nil {
		log.Fatalf("evaluate position: %v", err)
	}

	fmt.Printf("bestmove=%s ponder=%s depth=%d pv=%v\n", result.BestMove, result.Ponder, result.Depth, result.PV)
	for _, line := range result.Lines {
		fmt.Printf("line=%d depth=%d pv=%v ", line.MultiPV, line.Depth, line.PV)

		if line.ScoreCP != nil {
			fmt.Printf("cp=%d ", *line.ScoreCP)
		} else {
			fmt.Print("cp=nil ")
		}

		if line.Mate != nil {
			fmt.Printf("mate=%d ", *line.Mate)
		} else {
			fmt.Print("mate=nil ")
		}

		fmt.Println()
	}
}
