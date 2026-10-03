package main

import (
	"errors"
	"log"
	"net/http"
	"os"

	"private-release/internal/httpapi"
	"private-release/internal/service"
	"private-release/internal/store"
)

func main() {
	addr := env("ADDR", ":8080")
	dbPath := env("DB_PATH", "private-release.db")
	adminKey := os.Getenv("ADMIN_KEY")
	if adminKey == "" {
		log.Fatal("ADMIN_KEY is required")
	}
	db, err := store.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	svc := service.New(db)
	server := &http.Server{Addr: addr, Handler: httpapi.NewRouter(svc, adminKey)}
	log.Printf("listening on %s with database %s", addr, dbPath)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
