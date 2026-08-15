package symbols_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jolovicdev/anchor-db/internal/symbols"
)

func TestServiceExtractsGoSymbols(t *testing.T) {
	svc := symbols.NewService()
	content := []byte("package sample\n\ntype Worker struct{}\n\nfunc Add(a int, b int) int { return a + b }\n\nfunc (w *Worker) Run() error { return nil }\n")

	items, err := svc.Extract(context.Background(), "go", "sample.go", content)
	if err != nil {
		t.Fatalf("extract symbols: %v", err)
	}
	if len(items) < 3 {
		t.Fatalf("expected at least 3 symbols, got %d", len(items))
	}
}

func TestServiceReturnsEmptyForUnsupportedLanguage(t *testing.T) {
	svc := symbols.NewService()
	items, err := svc.Extract(context.Background(), "elixir", "sample.ex", []byte("defmodule Sample do\nend\n"))
	if err != nil {
		t.Fatalf("extract unsupported symbols: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected no symbols, got %d", len(items))
	}
}

func TestServiceExtractsPythonAndJavaScriptSymbols(t *testing.T) {
	svc := symbols.NewService()

	pythonItems, err := svc.Extract(context.Background(), "python", "sample.py", []byte("class Worker:\n    pass\n\ndef add(a, b):\n    return a + b\n"))
	if err != nil {
		t.Fatalf("extract python symbols: %v", err)
	}
	if len(pythonItems) < 2 {
		t.Fatalf("expected python class and function, got %d", len(pythonItems))
	}

	jsItems, err := svc.Extract(context.Background(), "javascript", "sample.js", []byte("class Worker {}\nfunction add(a, b) { return a + b }\n"))
	if err != nil {
		t.Fatalf("extract javascript symbols: %v", err)
	}
	if len(jsItems) < 2 {
		t.Fatalf("expected javascript class and function, got %d", len(jsItems))
	}

	tsItems, err := svc.Extract(context.Background(), "typescript", "sample.ts", []byte("class Worker {}\nfunction add(a: number, b: number): number { return a + b }\n"))
	if err != nil {
		t.Fatalf("extract typescript symbols: %v", err)
	}
	if len(tsItems) < 2 {
		t.Fatalf("expected typescript class and function, got %d", len(tsItems))
	}
}

const runtimeExtractorJSON = `[{"path":"notes.txt","language":"text","kind":"section","symbol_path":"Intro","start_line":1,"start_col":1,"end_line":1,"end_col":6}]`

// writeRuntimeExtractor writes a plugin that prints a fixed result. Windows will
// not run an extensionless file whatever its mode bits, so there it gets the
// extension and the interpreter that platform actually has.
func writeRuntimeExtractor(t *testing.T, dir string) {
	t.Helper()

	name, script := "symbols-text", "#!/bin/sh\ncat <<'EOF'\n"+runtimeExtractorJSON+"\nEOF\n"
	if runtime.GOOS == "windows" {
		name, script = "symbols-text.cmd", "@echo off\r\necho "+runtimeExtractorJSON+"\r\n"
	}

	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatalf("write extractor: %v", err)
	}
}

func TestServiceLoadsRuntimeExtractorExecutable(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeExtractor(t, dir)

	svc := symbols.NewService(symbols.WithExternalDir(dir))
	items, err := svc.Extract(context.Background(), "text", "notes.txt", []byte("Intro\n"))
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

// A plugin file that is present but cannot be run is the same situation as no
// plugin at all. Failing the whole extraction over it would take a repository's
// symbol resolution down for a stray file with the wrong mode bits.
func TestServiceIgnoresRuntimeExtractorThatIsNotExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the executable bit has no equivalent on Windows")
	}

	dir := t.TempDir()
	extractorPath := filepath.Join(dir, "symbols-text")
	if err := os.WriteFile(extractorPath, []byte("#!/bin/sh\necho []\n"), 0o644); err != nil {
		t.Fatalf("write extractor: %v", err)
	}

	svc := symbols.NewService(symbols.WithExternalDir(dir))
	items, err := svc.Extract(context.Background(), "text", "notes.txt", []byte("Intro\n"))
	if err != nil {
		t.Fatalf("extract with a non-executable plugin should not fail: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected no symbols, got %d", len(items))
	}
}
