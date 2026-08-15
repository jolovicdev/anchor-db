package symbols

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jolovicdev/anchor-db/internal/domain"
)

// externalTimeout bounds a symbol plugin so a hung helper cannot stall a sync
// pass indefinitely when the caller's context has no deadline of its own.
const externalTimeout = 30 * time.Second

func extractExternal(ctx context.Context, dir, language, path string, content []byte) ([]domain.Symbol, error) {
	if dir == "" {
		return []domain.Symbol{}, nil
	}
	// The language is part of a filename, so it must not be able to walk out of
	// the plugin directory.
	if language == "" || strings.ContainsAny(language, `/\`) || strings.Contains(language, "..") {
		return []domain.Symbol{}, nil
	}
	// LookPath rather than Stat: it checks that the file is actually executable
	// instead of merely present, and on Windows it resolves the extension, where
	// a bare "symbols-text" could never be run.
	commandPath, err := exec.LookPath(filepath.Join(dir, "symbols-"+language))
	if err != nil {
		return []domain.Symbol{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, externalTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, commandPath, path)
	cmd.Stdin = bytes.NewReader(content)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var symbols []domain.Symbol
	if err := json.Unmarshal(output, &symbols); err != nil {
		return nil, err
	}
	return symbols, nil
}
