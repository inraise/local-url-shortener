package main

import (
	"local-url-shortener/internal"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	port := getEnv("PORT", "8080")
	baseURL := getEnv("BASE_URL", "http://localhost:"+port)

	store := internal.NewMemoryStore()
	service := internal.NewService(store, nil)
	handler := internal.NewHandler(service, baseURL)

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("server listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
