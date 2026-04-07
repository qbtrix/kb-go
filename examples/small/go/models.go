// models.go — Domain models and storage interface for the task system.
package taskapi

import (
	"crypto/rand"
	"fmt"
	"time"
)

// Task statuses.
const (
	StatusOpen       = "open"
	StatusInProgress = "in_progress"
	StatusDone       = "done"
	StatusClosed     = "closed"
)

// Priority levels.
const (
	PriorityLow    = 1
	PriorityMedium = 2
	PriorityHigh   = 3
	PriorityCritical = 4
)

// Task represents a work item in the system.
type Task struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Priority    int       `json:"priority"`
	AssigneeID  string    `json:"assignee_id,omitempty"`
	Labels      []string  `json:"labels,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	DueDate     time.Time `json:"due_date,omitempty"`
}

// User represents a team member who can be assigned tasks.
type User struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	TeamID    string    `json:"team_id"`
	CreatedAt time.Time `json:"created_at"`
}

// Team groups users for task assignment.
type Team struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Members []string `json:"members"` // User IDs
}

// TaskStore defines the persistence interface for tasks.
type TaskStore interface {
	ListTasks(status string) ([]*Task, error)
	GetTask(id string) (*Task, error)
	CreateTask(task *Task) error
	UpdateTask(id string, updates map[string]any) (*Task, error)
	DeleteTask(id string) error
}

// IsOverdue returns true if the task is past its due date and not completed.
func (t *Task) IsOverdue() bool {
	if t.DueDate.IsZero() || t.Status == StatusDone || t.Status == StatusClosed {
		return false
	}
	return time.Now().After(t.DueDate)
}

func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}
