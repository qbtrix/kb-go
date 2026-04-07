// handler.go — HTTP handlers for task CRUD operations.
package taskapi

import (
	"encoding/json"
	"net/http"
	"time"
)

// TaskHandler holds dependencies for task-related HTTP handlers.
type TaskHandler struct {
	store TaskStore
}

// NewTaskHandler creates a handler backed by the given store.
func NewTaskHandler(store TaskStore) *TaskHandler {
	return &TaskHandler{store: store}
}

// CreateRequest is the payload for creating a new task.
type CreateRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	AssigneeID  string `json:"assignee_id,omitempty"`
	Priority    int    `json:"priority"`
}

// TaskResponse wraps a task for API output.
type TaskResponse struct {
	Task  *Task  `json:"task"`
	Error string `json:"error,omitempty"`
}

// List returns all tasks, optionally filtered by status.
func (h *TaskHandler) List(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	tasks, err := h.store.ListTasks(status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks, "count": len(tasks)})
}

// Create adds a new task.
func (h *TaskHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	task := &Task{
		ID:          generateID(),
		Title:       req.Title,
		Description: req.Description,
		Status:      StatusOpen,
		Priority:    req.Priority,
		AssigneeID:  req.AssigneeID,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	if err := h.store.CreateTask(task); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, TaskResponse{Task: task})
}

// Get returns a single task by ID.
func (h *TaskHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := h.store.GetTask(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	writeJSON(w, http.StatusOK, TaskResponse{Task: task})
}

// Update modifies an existing task.
func (h *TaskHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var updates map[string]any
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	task, err := h.store.UpdateTask(id, updates)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	writeJSON(w, http.StatusOK, TaskResponse{Task: task})
}

// Delete removes a task by ID.
func (h *TaskHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.store.DeleteTask(id); err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
