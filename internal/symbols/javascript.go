package symbols

import (
	"context"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"

	"anchordb/internal/domain"
)

type javascriptExtractor struct {
	language *sitter.Language
}

func newJavaScriptExtractor() *javascriptExtractor {
	return &javascriptExtractor{
		language: sitter.NewLanguage(tree_sitter_javascript.Language()),
	}
}

func (j *javascriptExtractor) Extract(_ context.Context, path string, content []byte) ([]domain.Symbol, error) {
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(j.language); err != nil {
		return nil, err
	}
	tree := parser.Parse(content, nil)
	defer tree.Close()
	root := tree.RootNode()
	cursor := root.Walk()
	defer cursor.Close()

	return walkNamed(path, "javascript", content, root, cursor, map[string]string{
		"function_declaration": "function",
		"class_declaration":    "class",
	}), nil
}
