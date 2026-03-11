package main

import (
	"context"
	"flag"
	"log"
	"net/http"
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
	syncer.Start(context.Background(), *syncInterval)

	log.Printf("anchord listening on %s", *listen)
	log.Fatal(http.ListenAndServe(*listen, api.NewServer(service)))
}
