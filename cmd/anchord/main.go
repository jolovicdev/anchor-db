package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"anchordb/internal/api"
	"anchordb/internal/app"
	"anchordb/internal/jobs"
	sqlitestore "anchordb/internal/store/sqlite"
)

func main() {
	listen := flag.String("listen", ":7740", "")
	dbPath := flag.String("db", "./anchor.db", "")
	syncInterval := flag.Duration("sync-interval", 30*time.Second, "")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := sqlitestore.Open(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	service, err := app.NewService(store)
	if err != nil {
		log.Fatal(err)
	}

	syncer := jobs.NewSyncer(service)
	syncer.Start(ctx, *syncInterval)

	log.Printf("anchord listening on %s", *listen)
	server := &http.Server{
		Addr:    *listen,
		Handler: api.NewServer(service),
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
	}()

	err = server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
