// server.go — HTTP server for the task management API.
package taskapi

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"
)

// DefaultPort is the default HTTP listen port.
const DefaultPort = 8080

// ShutdownTimeout is how long to wait for graceful shutdown.
const ShutdownTimeout = 10 * time.Second

// Server wraps an HTTP server with lifecycle management.
type Server struct {
	Port    int
	Router  *http.ServeMux
	handler *TaskHandler
	srv     *http.Server
}

// NewServer creates a configured server with all routes registered.
func NewServer(store TaskStore, cfg *Config) *Server {
	mux := http.NewServeMux()
	handler := NewTaskHandler(store)

	s := &Server{
		Port:    cfg.Port,
		Router:  mux,
		handler: handler,
	}

	s.registerRoutes()
	return s
}

// registerRoutes wires up all API endpoints.
func (s *Server) registerRoutes() {
	s.Router.HandleFunc("GET /tasks", s.handler.List)
	s.Router.HandleFunc("POST /tasks", s.handler.Create)
	s.Router.HandleFunc("GET /tasks/{id}", s.handler.Get)
	s.Router.HandleFunc("PUT /tasks/{id}", s.handler.Update)
	s.Router.HandleFunc("DELETE /tasks/{id}", s.handler.Delete)
	s.Router.HandleFunc("GET /health", HealthCheck)
}

// Start boots the HTTP server and blocks until shutdown.
func (s *Server) Start() error {
	s.srv = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.Port),
		Handler:      LoggingMiddleware(AuthMiddleware(s.Router)),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}
	log.Printf("Server starting on port %d", s.Port)
	return s.srv.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()
	log.Println("Server shutting down...")
	return s.srv.Shutdown(ctx)
}

// HealthCheck returns a simple health response.
func HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"ok","time":"%s"}`, time.Now().UTC().Format(time.RFC3339))
}
