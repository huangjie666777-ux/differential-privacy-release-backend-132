package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"private-release/internal/httpapi"
	"private-release/internal/store"
)

func main() {
	addr := flag.String("addr", envOr("ADDR", ":8080"), "listen address")
	dbPath := flag.String("db", envOr("DB_PATH", "release.db"), "SQLite database path")
	adminKey := flag.String("admin-key", os.Getenv("ADMIN_KEY"), "admin API key (or ADMIN_KEY env)")
	flag.Parse()

	if *adminKey == "" {
		log.Fatal("admin key required: set ADMIN_KEY or pass -admin-key")
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	srv := httpapi.NewServer(st, *adminKey)
	log.Printf("listening on %s, db=%s", *addr, *dbPath)
	log.Fatal(http.ListenAndServe(*addr, srv.Router()))
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
