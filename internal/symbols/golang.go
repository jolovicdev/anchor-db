package symbols

import (
	"context"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"

	"github.com/jolovicdev/anchor-db/internal/code"
	"github.com/jolovicdev/anchor-db/internal/domain"
)

type goExtractor struct {
	language *sitter.Language
}

func newGoExtractor() *goExtractor {
	return &goExtractor{
		language: sitter.NewLanguage(tree_sitter_go.Language()),
	}
}

func (g *goExtractor) Extract(ctx context.Context, path string, content []byte) ([]domain.Symbol, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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

	index := code.NewPositionIndex(string(content))
	var symbols []domain.Symbol
	symbols = append(symbols, g.walk(index, path, content, root, cursor)...)
	return symbols, nil
}

func (g *goExtractor) walk(index *code.PositionIndex, path string, content []byte, node *sitter.Node, cursor *sitter.TreeCursor) []domain.Symbol {
	if node == nil {
		return nil
	}
	var symbols []domain.Symbol
	switch node.Kind() {
	case "function_declaration":
		name := textFor(node.ChildByFieldName("name"), content)
		if name != "" {
			symbols = append(symbols, symbolFor(index, path, "go", "function", name, node))
		}
	case "method_declaration":
		name := textFor(node.ChildByFieldName("name"), content)
		receiver := normalizeReceiver(textFor(node.ChildByFieldName("receiver"), content))
		symbolPath := name
		if receiver != "" {
			symbolPath = receiver + "." + name
		}
		if name != "" {
			symbols = append(symbols, symbolFor(index, path, "go", "method", symbolPath, node))
		}
	case "type_spec":
		name := textFor(node.ChildByFieldName("name"), content)
		if name != "" {
			symbols = append(symbols, symbolFor(index, path, "go", "type", name, node))
		}
	}

	for _, child := range node.NamedChildren(cursor) {
		childCopy := child
		symbols = append(symbols, g.walk(index, path, content, &childCopy, cursor)...)
	}
	return symbols
}

// symbolFor is the single place tree-sitter positions become domain spans.
// Tree-sitter reports columns in bytes, so they are converted through the
// position index rather than used directly -- see the code package docs.
func symbolFor(index *code.PositionIndex, path, language, kind, symbolPath string, node *sitter.Node) domain.Symbol {
	startLine, startCol := index.LineCol(int(node.StartByte()))
	endLine, endCol := index.LineCol(int(node.EndByte()))
	return domain.Symbol{
		Path:       path,
		Language:   language,
		Kind:       kind,
		SymbolPath: symbolPath,
		StartLine:  startLine,
		StartCol:   startCol,
		EndLine:    endLine,
		EndCol:     endCol,
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
	// A pointer receiver is written "(s *Service)". Keeping the star put it in
	// the stored symbol path, so an exact filter or search for "Service.Resolve"
	// missed "*Service.Resolve", and the star leaked into anything that displayed
	// it. Pointerness is a property of the receiver, not part of the type's name.
	return strings.TrimPrefix(fields[len(fields)-1], "*")
}
