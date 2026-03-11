package jobs

import (
	"context"
	"time"

	"github.com/jolovicdev/anchor-db/internal/app"
)

type Syncer struct {
	service *app.Service
}

func NewSyncer(service *app.Service) *Syncer {
	return &Syncer{service: service}
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
				_ = s.RunOnce(ctx)
			}
		}
	}()
}
