package stockfish

import (
	"runtime"
	"testing"
)

func TestValidateConfigDefaults(t *testing.T) {
	cfg, err := validateConfig(Config{BinaryPath: "sh"})
	if err != nil {
		t.Fatalf("validateConfig() error = %v", err)
	}
	if cfg.poolSize != 1 {
		t.Fatalf("poolSize = %d, want 1", cfg.poolSize)
	}
	if cfg.queueSize != 4 {
		t.Fatalf("queueSize = %d, want 4", cfg.queueSize)
	}
	if cfg.perEngineThreads != 1 {
		t.Fatalf("perEngineThreads = %d, want 1", cfg.perEngineThreads)
	}
	if cfg.perEngineHashMB != 16 {
		t.Fatalf("perEngineHashMB = %d, want 16", cfg.perEngineHashMB)
	}
	if cfg.maxMultiPV != 5 {
		t.Fatalf("maxMultiPV = %d, want 5", cfg.maxMultiPV)
	}
}

func TestValidateConfigRejectsCPUOvercommit(t *testing.T) {
	_, err := validateConfig(Config{
		BinaryPath: "sh",
		PoolSize:   runtime.NumCPU() + 1,
	})
	if err == nil {
		t.Fatalf("expected overcommit error, got nil")
	}
}

func TestValidateConfigDerivesHashFromTotal(t *testing.T) {
	cfg, err := validateConfig(Config{
		BinaryPath:               "sh",
		PoolSize:                 2,
		TotalHashMB:              64,
		AllowUnsafeCPUOvercommit: true,
	})
	if err != nil {
		t.Fatalf("validateConfig() error = %v", err)
	}
	if cfg.perEngineHashMB != 32 {
		t.Fatalf("perEngineHashMB = %d, want 32", cfg.perEngineHashMB)
	}
}

func TestValidateConfigRejectsNegativeMaxMultiPV(t *testing.T) {
	_, err := validateConfig(Config{
		BinaryPath: "sh",
		MaxMultiPV: -1,
	})
	if err == nil {
		t.Fatalf("expected max multipv validation error, got nil")
	}
}

func TestNormalizeFEN(t *testing.T) {
	fen, err := normalizeFEN("  rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1  ")
	if err != nil {
		t.Fatalf("normalizeFEN() error = %v", err)
	}
	if fen != "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1" {
		t.Fatalf("normalizeFEN() = %q", fen)
	}
}

func TestNormalizeFENRejectsNewLine(t *testing.T) {
	_, err := normalizeFEN("8/8/8/8/8/8/8/8 w - - 0 1\nisready")
	if err == nil {
		t.Fatalf("expected newline rejection, got nil")
	}
}
