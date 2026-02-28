package stockfish

import (
	"strconv"
	"strings"
)

type infoUpdate struct {
	MultiPV *int
	Depth   *int
	ScoreCP *int
	Mate    *int
	PV      []string
}

func parseBestMoveLine(line string) (bestMove string, ponder string, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "bestmove" {
		return "", "", false
	}
	bestMove = fields[1]
	for i := 2; i+1 < len(fields); i++ {
		if fields[i] == "ponder" {
			ponder = fields[i+1]
			break
		}
	}
	return bestMove, ponder, true
}

func parseInfoLine(line string) (infoUpdate, bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "info" {
		return infoUpdate{}, false
	}

	update := infoUpdate{}
	for i := 1; i < len(fields); i++ {
		switch fields[i] {
		case "multipv":
			if i+1 < len(fields) {
				if multiPV, err := strconv.Atoi(fields[i+1]); err == nil {
					update.MultiPV = intPtr(multiPV)
				}
				i++
			}
		case "depth":
			if i+1 < len(fields) {
				if depth, err := strconv.Atoi(fields[i+1]); err == nil {
					update.Depth = intPtr(depth)
				}
				i++
			}
		case "score":
			if i+2 < len(fields) {
				scoreType := fields[i+1]
				if value, err := strconv.Atoi(fields[i+2]); err == nil {
					if scoreType == "cp" {
						update.ScoreCP = intPtr(value)
					}
					if scoreType == "mate" {
						update.Mate = intPtr(value)
					}
				}
				i += 2
			}
		case "pv":
			if i+1 < len(fields) {
				update.PV = append([]string(nil), fields[i+1:]...)
			}
			return update, true
		}
	}

	return update, true
}

func intPtr(value int) *int {
	return &value
}
