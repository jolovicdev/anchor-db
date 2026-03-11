package symbols

import (
	"context"
	"os"

	"github.com/jolovicdev/anchor-db/internal/domain"
)

type Option func(*Service)

type Extractor interface {
	Extract(ctx context.Context, path string, content []byte) ([]domain.Symbol, error)
}

type Service struct {
	extractors  map[string]Extractor
	externalDir string
}

func NewService(options ...Option) *Service {
	service := &Service{
		extractors: map[string]Extractor{
			"go":         newGoExtractor(),
			"python":     newPythonExtractor(),
			"javascript": newJavaScriptExtractor(),
			"typescript": newTypeScriptExtractor(),
		},
	}
	for _, option := range options {
		option(service)
	}
	if service.externalDir == "" {
		service.externalDir = os.Getenv("ANCHORDB_SYMBOL_PLUGINS_DIR")
	}
	return service
}

func WithExternalDir(dir string) Option {
	return func(service *Service) {
		service.externalDir = dir
	}
}

func (s *Service) Extract(language, path string, content []byte) ([]domain.Symbol, error) {
	extractor, ok := s.extractors[language]
	if !ok {
		return extractExternal(s.externalDir, language, path, content)
	}
	return extractor.Extract(context.Background(), path, content)
}
