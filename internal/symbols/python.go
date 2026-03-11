package symbols

import (
	"context"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"

	"github.com/jolovicdev/anchor-db/internal/domain"
)

type pythonExtractor struct {
	language *sitter.Language
}

func newPythonExtractor() *pythonExtractor {
	return &pythonExtractor{
		language: sitter.NewLanguage(tree_sitter_python.Language()),
	}
}

func (p *pythonExtractor) Extract(_ context.Context, path string, content []byte) ([]domain.Symbol, error) {
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(p.language); err != nil {
		return nil, err
	}
	tree := parser.Parse(content, nil)
	defer tree.Close()
	root := tree.RootNode()
	cursor := root.Walk()
	defer cursor.Close()

	return walkNamed(path, "python", content, root, cursor, map[string]string{
		"function_definition": "function",
		"class_definition":    "class",
	}), nil
}

func walkNamed(path, language string, content []byte, node *sitter.Node, cursor *sitter.TreeCursor, kinds map[string]string) []domain.Symbol {
	if node == nil {
		return nil
	}
	var symbols []domain.Symbol
	if symbolKind, ok := kinds[node.Kind()]; ok {
		name := strings.TrimSpace(textFor(node.ChildByFieldName("name"), content))
		if name != "" {
			symbols = append(symbols, symbolFor(path, language, symbolKind, name, node))
		}
	}
	for _, child := range node.NamedChildren(cursor) {
		childCopy := child
		symbols = append(symbols, walkNamed(path, language, content, &childCopy, cursor, kinds)...)
	}
	return symbols
}
