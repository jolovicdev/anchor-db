package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/jolovicdev/anchor-db/internal/api"
	"github.com/jolovicdev/anchor-db/internal/app"
	"github.com/jolovicdev/anchor-db/internal/jobs"
	sqlitestore "github.com/jolovicdev/anchor-db/internal/store/sqlite"
	"github.com/jolovicdev/anchor-db/internal/version"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:7740", "address to listen on")
	dbPath := flag.String("db", "./anchor.db", "path to the AnchorDB database")
	syncInterval := flag.Duration("sync-interval", 30*time.Second, "how often to re-resolve anchors")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("anchord %s\n", version.String())
		return
	}

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
		Handler: api.NewServer(service, *listen),
		// Without these a stalled or slow client holds a connection open
		// indefinitely. WriteTimeout is the most generous of the three because
		// a sync triggered over HTTP has to shell out to git.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
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
