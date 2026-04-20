package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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

	eventType := r.Header.Get("X-GitHub-Event")
	if eventType != "push" {
		log.Printf("Webhook received and ignored: event=%s (only push is handled)", eventType)
		w.WriteHeader(http.StatusOK)
		return
	}

	var payload Payload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "Bad JSON", http.StatusBadRequest)
		return
	}

	if payload.Ref != cfg.TargetRef {
		log.Printf("Webhook received and ignored: ref=%s (target ref=%s)", payload.Ref, cfg.TargetRef)
		w.WriteHeader(http.StatusOK)
		return
	}

	log.Println("Received matching push webhook; triggering repository update")

	// 🚀 trigger async update (non-blocking)
	triggerUpdate()

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
	gitDir := filepath.Join(cfg.RepoDir, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	if _, err := os.Stat(cfg.RepoDir); err == nil {
		entries, readErr := os.ReadDir(cfg.RepoDir)
		if readErr != nil {
			return readErr
		}

		if len(entries) > 0 {
			return fmt.Errorf("%s exists but is not a git repo (.git missing); clean this directory or set REPO_DIR to an empty path", cfg.RepoDir)
		}
	} else if os.IsNotExist(err) {
		if mkErr := os.MkdirAll(cfg.RepoDir, 0o755); mkErr != nil {
			return mkErr
		}
	} else {
		return err
	}

	log.Println("Cloning repo...")
	cmd := exec.Command("git", "clone", "--branch", cfg.TargetBranch, cfg.RepoURL, cfg.RepoDir)
	cmd.Stdout = log.Writer()
	cmd.Stderr = log.Writer()
	return cmd.Run()
}

func runGit(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Stdout = log.Writer()
	cmd.Stderr = log.Writer()
	return cmd.Run()
}

func updateRepo() error {
	log.Println("Fetching latest changes...")
	if err := runGit("-C", cfg.RepoDir, "fetch", "--prune", "origin", cfg.TargetBranch); err != nil {
		return err
	}

	log.Println("Resetting working tree to remote branch...")
	if err := runGit("-C", cfg.RepoDir, "reset", "--hard", "FETCH_HEAD"); err != nil {
		return err
	}

	log.Println("Removing untracked files...")
	return runGit("-C", cfg.RepoDir, "clean", "-fd")
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
