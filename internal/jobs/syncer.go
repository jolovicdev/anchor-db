package jobs

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/jolovicdev/anchor-db/internal/app"
)

type Syncer struct {
	service *app.Service
	logf    func(string, ...any)
}

func NewSyncer(service *app.Service) *Syncer {
	return &Syncer{service: service, logf: log.Printf}
}

// WithLogger overrides where background sync failures are reported.
func (s *Syncer) WithLogger(logf func(string, ...any)) *Syncer {
	if logf != nil {
		s.logf = logf
	}
	return s
}

func (s *Syncer) RunOnce(ctx context.Context) error {
	return s.service.ResolveAll(ctx)
}

func (s *Syncer) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Report failures instead of discarding them: a background
				// loop that silently stops resolving anchors looks identical
				// to one with nothing to do.
				if err := s.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
					s.logf("anchordb: background sync: %v", err)
				}
			}
		}
	}()
}
