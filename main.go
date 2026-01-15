package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	defaultPort   = "3333"
	defaultDBPath = "grinder.db"
)

func main() {
	// Get configuration from environment
	port := os.Getenv("GRINDER_PORT")
	if port == "" {
		port = defaultPort
	}

	dbPath := os.Getenv("GRINDER_DB")
	if dbPath == "" {
		dbPath = defaultDBPath
	}

	// Initialize database
	log.Println("Initializing database...")
	db, err := NewDatabase(dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Mark any tasks that were in progress as blocked (server restart)
	if err := db.MarkRunningTasksAsBlocked("Server was restarted"); err != nil {
		log.Printf("Warning: Failed to mark running tasks as blocked: %v", err)
	}

	// Initialize WebSocket hub
	hub := NewHub()
	go hub.Run()

	// Initialize RALPH runner
	runner := NewRalphRunner(db, hub)

	// Initialize handlers
	handler := NewHandler(db, hub, runner)

	// Setup routes
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/tasks", handler.HandleTasks)
	mux.HandleFunc("/api/tasks/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, "/pause") {
			handler.HandleTaskPause(w, r)
		} else if strings.HasSuffix(path, "/resume") {
			handler.HandleTaskResume(w, r)
		} else if strings.HasSuffix(path, "/stop") {
			handler.HandleTaskStop(w, r)
		} else if strings.HasSuffix(path, "/feedback") {
			handler.HandleTaskFeedback(w, r)
		} else if strings.HasSuffix(path, "/deploy") {
			handler.HandleDeployTask(w, r)
		} else {
			handler.HandleTask(w, r)
		}
	})
	mux.HandleFunc("/api/config", handler.HandleConfig)

	// Directory browsing routes
	mux.HandleFunc("/api/browse", handler.HandleBrowse)
	mux.HandleFunc("/api/browse/create", handler.HandleCreateDir)

	// GitHub routes
	mux.HandleFunc("/api/github/validate", handler.HandleGitHubValidate)

	// Project routes
	mux.HandleFunc("/api/projects", handler.HandleProjects)
	mux.HandleFunc("/api/projects/scan", handler.HandleProjectScan)
	mux.HandleFunc("/api/projects/scan-all", handler.HandleScanAllProjects)
	mux.HandleFunc("/api/projects/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, "/git-init") {
			handler.HandleGitInit(w, r)
		} else if strings.HasSuffix(path, "/github-repo") {
			handler.HandleCreateGitHubRepo(w, r)
		} else if strings.HasSuffix(path, "/git-info") {
			handler.getProjectGitInfo(w, r)
		} else if strings.HasSuffix(path, "/branches") {
			handler.getProjectBranches(w, r)
		} else if strings.HasSuffix(path, "/rules") {
			handler.HandleBranchRules(w, r)
		} else if strings.Contains(path, "/rules/") {
			handler.HandleBranchRule(w, r)
		} else {
			handler.HandleProject(w, r)
		}
	})

	// Task type routes
	mux.HandleFunc("/api/task-types", handler.HandleTaskTypes)
	mux.HandleFunc("/api/task-types/", handler.HandleTaskType)

	// WebSocket route
	mux.HandleFunc("/ws", hub.ServeWs)

	// Static files
	staticFS := http.FileServer(http.Dir("static"))
	mux.Handle("/", staticFS)

	// Create server
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      corsMiddleware(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Print startup banner
	fmt.Println()
	fmt.Println("  GRINDER v1.0")
	fmt.Printf("  Server running on http://localhost:%s\n", port)
	fmt.Println()

	// Start server in a goroutine
	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down...")

	// Stop all running RALPH processes
	runner.StopAll()

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("GRINDER stopped")
}

// corsMiddleware adds CORS headers for local development
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
