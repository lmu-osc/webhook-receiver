package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/webhook", webhookHandler)
	log.Printf("Starting server on :%d", cfg.ServePort)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", cfg.ServePort), nil); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
