package stockfish

import "testing"

func TestParseBestMoveLine(t *testing.T) {
	best, ponder, ok := parseBestMoveLine("bestmove e2e4 ponder e7e5")
	if !ok {
		t.Fatalf("parseBestMoveLine returned ok=false")
	}
	if best != "e2e4" {
		t.Fatalf("best = %q, want e2e4", best)
	}
	if ponder != "e7e5" {
		t.Fatalf("ponder = %q, want e7e5", ponder)
	}
}

func TestParseInfoLineCP(t *testing.T) {
	update, ok := parseInfoLine("info multipv 2 depth 18 score cp 34 pv e2e4 e7e5 g1f3")
	if !ok {
		t.Fatalf("parseInfoLine returned ok=false")
	}
	if update.MultiPV == nil || *update.MultiPV != 2 {
		t.Fatalf("multipv = %v, want 2", update.MultiPV)
	}
	if update.Depth == nil || *update.Depth != 18 {
		t.Fatalf("depth = %v, want 18", update.Depth)
	}
	if update.ScoreCP == nil || *update.ScoreCP != 34 {
		t.Fatalf("score cp = %v, want 34", update.ScoreCP)
	}
	if len(update.PV) != 3 {
		t.Fatalf("pv len = %d, want 3", len(update.PV))
	}
}

func TestParseInfoLineMate(t *testing.T) {
	update, ok := parseInfoLine("info depth 22 score mate -3 pv h7h8q")
	if !ok {
		t.Fatalf("parseInfoLine returned ok=false")
	}
	if update.MultiPV != nil {
		t.Fatalf("multipv = %v, want nil", update.MultiPV)
	}
	if update.Mate == nil || *update.Mate != -3 {
		t.Fatalf("mate = %v, want -3", update.Mate)
	}
}
