package main

import (
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/webhook", webhookHandler)
	log.Println("Starting server on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
