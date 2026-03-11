package symbols

import (
	"context"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

	"anchordb/internal/domain"
)

type typeScriptExtractor struct {
	language *sitter.Language
}

func newTypeScriptExtractor() *typeScriptExtractor {
	return &typeScriptExtractor{
		language: sitter.NewLanguage(tree_sitter_typescript.LanguageTypescript()),
	}
}

func (t *typeScriptExtractor) Extract(_ context.Context, path string, content []byte) ([]domain.Symbol, error) {
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(t.language); err != nil {
		return nil, err
	}
	tree := parser.Parse(content, nil)
	defer tree.Close()
	root := tree.RootNode()
	cursor := root.Walk()
	defer cursor.Close()

	return walkNamed(path, "typescript", content, root, cursor, map[string]string{
		"function_declaration": "function",
		"class_declaration":    "class",
	}), nil
}
