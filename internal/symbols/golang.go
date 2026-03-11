package symbols

import (
	"context"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"

	"anchordb/internal/domain"
)

type goExtractor struct {
	language *sitter.Language
}

func newGoExtractor() *goExtractor {
	return &goExtractor{
		language: sitter.NewLanguage(tree_sitter_go.Language()),
	}
}

func (g *goExtractor) Extract(_ context.Context, path string, content []byte) ([]domain.Symbol, error) {
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(g.language); err != nil {
		return nil, err
	}
	tree := parser.Parse(content, nil)
	defer tree.Close()
	root := tree.RootNode()

	cursor := root.Walk()
	defer cursor.Close()

	var symbols []domain.Symbol
	symbols = append(symbols, g.walk(path, content, root, cursor)...)
	return symbols, nil
}

func (g *goExtractor) walk(path string, content []byte, node *sitter.Node, cursor *sitter.TreeCursor) []domain.Symbol {
	if node == nil {
		return nil
	}
	var symbols []domain.Symbol
	switch node.Kind() {
	case "function_declaration":
		name := textFor(node.ChildByFieldName("name"), content)
		if name != "" {
			symbols = append(symbols, symbolFor(path, "go", "function", name, node))
		}
	case "method_declaration":
		name := textFor(node.ChildByFieldName("name"), content)
		receiver := normalizeReceiver(textFor(node.ChildByFieldName("receiver"), content))
		symbolPath := name
		if receiver != "" {
			symbolPath = receiver + "." + name
		}
		if name != "" {
			symbols = append(symbols, symbolFor(path, "go", "method", symbolPath, node))
		}
	case "type_spec":
		name := textFor(node.ChildByFieldName("name"), content)
		if name != "" {
			symbols = append(symbols, symbolFor(path, "go", "type", name, node))
		}
	}

	for _, child := range node.NamedChildren(cursor) {
		childCopy := child
		symbols = append(symbols, g.walk(path, content, &childCopy, cursor)...)
	}
	return symbols
}

func symbolFor(path, language, kind, symbolPath string, node *sitter.Node) domain.Symbol {
	start := node.StartPosition()
	end := node.EndPosition()
	return domain.Symbol{
		Path:       path,
		Language:   language,
		Kind:       kind,
		SymbolPath: symbolPath,
		StartLine:  int(start.Row) + 1,
		StartCol:   int(start.Column) + 1,
		EndLine:    int(end.Row) + 1,
		EndCol:     int(end.Column) + 1,
	}
}

func textFor(node *sitter.Node, content []byte) string {
	if node == nil {
		return ""
	}
	return strings.TrimSpace(node.Utf8Text(content))
}

func normalizeReceiver(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "(")
	value = strings.TrimSuffix(value, ")")
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}
