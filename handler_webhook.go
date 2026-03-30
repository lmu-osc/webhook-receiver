package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"

	"github.com/joho/godotenv"
)

type Config struct {
	Secret       string
	RepoDir      string
	RepoURL      string
	TargetRef    string
	TargetBranch string
	ServePort    int
}

var cfg = mustLoadConfig()

// 🔒 concurrency control
var (
	mu      sync.Mutex
	running = false
	pending = false
)

type Payload struct {
	Ref string `json:"ref"`
}

func mustLoadConfig() Config {
	err := godotenv.Load(".env")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("failed loading .env: %v", err)
	}

	secret := os.Getenv("WEBHOOK_SECRET")
	if secret == "" {
		log.Fatal("missing required environment variable: WEBHOOK_SECRET")
	}

	repoURL := os.Getenv("REPO_URL")
	if repoURL == "" {
		log.Fatal("missing required environment variable: REPO_URL")
	}

	repoDir := os.Getenv("REPO_DIR")
	if repoDir == "" {
		repoDir = "./repo"
	}

	targetRef := os.Getenv("TARGET_REF")
	if targetRef == "" {
		targetRef = "refs/heads/gh-pages"
	}

	targetBranch := os.Getenv("TARGET_BRANCH")
	if targetBranch == "" {
		targetBranch = "gh-pages"
	}

	servePort := 8080
	if portStr := os.Getenv("SERVE_PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			servePort = port
		}
	}

	return Config{
		Secret:       secret,
		RepoDir:      repoDir,
		RepoURL:      repoURL,
		TargetRef:    targetRef,
		TargetBranch: targetBranch,
		ServePort:    servePort,
	}
}

func webhookHandler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	if !verifySignature(body, r.Header.Get("X-Hub-Signature-256")) {
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	if r.Header.Get("X-GitHub-Event") != "push" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var payload Payload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "Bad JSON", http.StatusBadRequest)
		return
	}

	if payload.Ref == cfg.TargetRef {
		log.Println("Received gh-pages update webhook")

		// 🚀 trigger async update (non-blocking)
		triggerUpdate()
	}

	// ✅ respond immediately to GitHub
	w.WriteHeader(http.StatusOK)
}

func verifySignature(body []byte, signature string) bool {
	mac := hmac.New(sha256.New, []byte(cfg.Secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

func ensureRepo() error {
	if _, err := os.Stat(cfg.RepoDir); os.IsNotExist(err) {
		log.Println("Cloning repo...")
		cmd := exec.Command("git", "clone", "--branch", cfg.TargetBranch, cfg.RepoURL, cfg.RepoDir)
		cmd.Stdout = log.Writer()
		cmd.Stderr = log.Writer()
		return cmd.Run()
	}
	return nil
}

func updateRepo() error {
	log.Println("Pulling latest changes...")
	cmd := exec.Command("git", "-C", cfg.RepoDir, "pull", "origin", cfg.TargetBranch)
	cmd.Stdout = log.Writer()
	cmd.Stderr = log.Writer()
	return cmd.Run()
}

// 🔁 runs one update cycle
func runUpdate() {
	if err := ensureRepo(); err != nil {
		log.Println("Clone failed:", err)
		return
	}

	if err := updateRepo(); err != nil {
		log.Println("Pull failed:", err)
		return
	}

	log.Println("Update complete")
}

// 🧠 smart scheduler (no overlap + coalescing)
func triggerUpdate() {
	mu.Lock()
	if running {
		pending = true
		mu.Unlock()
		return
	}
	running = true
	mu.Unlock()

	go func() {
		for {
			runUpdate()

			mu.Lock()
			if !pending {
				running = false
				mu.Unlock()
				return
			}
			pending = false
			mu.Unlock()
		}
	}()
}
