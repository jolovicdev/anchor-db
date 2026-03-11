package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/jolovicdev/anchor-db/internal/app"
	"github.com/jolovicdev/anchor-db/internal/mcpserver"
	sqlitestore "github.com/jolovicdev/anchor-db/internal/store/sqlite"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	dbPath := flag.String("db", "./anchor.db", "")
	flag.Parse()

	store, err := sqlitestore.Open(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer store.Close()

	service, err := app.NewService(store)
	if err != nil {
		log.Fatalf("new app service: %v", err)
	}

	server := mcpserver.New(service)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Printf("mcp server stopped: %v", err)
		os.Exit(1)
	}
}
