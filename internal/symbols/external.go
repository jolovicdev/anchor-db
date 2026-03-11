package symbols

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"

	"anchordb/internal/domain"
)

func extractExternal(dir, language, path string, content []byte) ([]domain.Symbol, error) {
	if dir == "" {
		return []domain.Symbol{}, nil
	}
	commandPath := filepath.Join(dir, "symbols-"+language)
	if _, err := os.Stat(commandPath); err != nil {
		return []domain.Symbol{}, nil
	}
	cmd := exec.CommandContext(context.Background(), commandPath, path)
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
