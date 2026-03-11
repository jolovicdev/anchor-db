package symbols_test

import (
	"os"
	"path/filepath"
	"testing"

	"anchordb/internal/symbols"
)

func TestServiceExtractsGoSymbols(t *testing.T) {
	svc := symbols.NewService()
	content := []byte("package sample\n\ntype Worker struct{}\n\nfunc Add(a int, b int) int { return a + b }\n\nfunc (w *Worker) Run() error { return nil }\n")

	items, err := svc.Extract("go", "sample.go", content)
	if err != nil {
		t.Fatalf("extract symbols: %v", err)
	}
	if len(items) < 3 {
		t.Fatalf("expected at least 3 symbols, got %d", len(items))
	}
}

func TestServiceReturnsEmptyForUnsupportedLanguage(t *testing.T) {
	svc := symbols.NewService()
	items, err := svc.Extract("elixir", "sample.ex", []byte("defmodule Sample do\nend\n"))
	if err != nil {
		t.Fatalf("extract unsupported symbols: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected no symbols, got %d", len(items))
	}
}

func TestServiceExtractsPythonAndJavaScriptSymbols(t *testing.T) {
	svc := symbols.NewService()

	pythonItems, err := svc.Extract("python", "sample.py", []byte("class Worker:\n    pass\n\ndef add(a, b):\n    return a + b\n"))
	if err != nil {
		t.Fatalf("extract python symbols: %v", err)
	}
	if len(pythonItems) < 2 {
		t.Fatalf("expected python class and function, got %d", len(pythonItems))
	}

	jsItems, err := svc.Extract("javascript", "sample.js", []byte("class Worker {}\nfunction add(a, b) { return a + b }\n"))
	if err != nil {
		t.Fatalf("extract javascript symbols: %v", err)
	}
	if len(jsItems) < 2 {
		t.Fatalf("expected javascript class and function, got %d", len(jsItems))
	}

	tsItems, err := svc.Extract("typescript", "sample.ts", []byte("class Worker {}\nfunction add(a: number, b: number): number { return a + b }\n"))
	if err != nil {
		t.Fatalf("extract typescript symbols: %v", err)
	}
	if len(tsItems) < 2 {
		t.Fatalf("expected typescript class and function, got %d", len(tsItems))
	}
}

func TestServiceLoadsRuntimeExtractorExecutable(t *testing.T) {
	dir := t.TempDir()
	extractorPath := filepath.Join(dir, "symbols-text")
	script := "#!/usr/bin/env bash\ncat <<'EOF'\n[{\"path\":\"notes.txt\",\"language\":\"text\",\"kind\":\"section\",\"symbol_path\":\"Intro\",\"start_line\":1,\"start_col\":1,\"end_line\":1,\"end_col\":6}]\nEOF\n"
	if err := os.WriteFile(extractorPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write extractor: %v", err)
	}

	svc := symbols.NewService(symbols.WithExternalDir(dir))
	items, err := svc.Extract("text", "notes.txt", []byte("Intro\n"))
	if err != nil {
		t.Fatalf("extract runtime symbols: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one runtime symbol, got %d", len(items))
	}
	if items[0].SymbolPath != "Intro" {
		t.Fatalf("expected Intro symbol, got %s", items[0].SymbolPath)
	}
}
