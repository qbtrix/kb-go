---
{
  "title": "Task API Domain Models and Storage Interface",
  "summary": "The models.go file defines the core domain entities for a task management system, including Task, User, and Team types, along with a TaskStore interface for persistence. It provides status and priority constants, and includes an IsOverdue() method for tracking task deadlines.",
  "concepts": [
    "Domain models",
    "Task management",
    "User assignment",
    "Team organization",
    "Status lifecycle",
    "Priority levels",
    "Persistence interface",
    "Repository pattern",
    "Deadline tracking",
    "Entity modeling"
  ],
  "categories": [
    "Backend Architecture",
    "Domain-Driven Design",
    "Go Programming",
    "Data Models",
    "Task Management Systems",
    "API Design"
  ],
  "source_docs": [
    "6ec8775206db89ef"
  ],
  "backlinks": null,
  "word_count": 480,
  "compiled_at": "2026-04-07T06:38:56Z",
  "compiled_with": "claude-haiku-4-5-20251001",
  "version": 1
}
---

# Task API Domain Models and Storage Interface

## Overview

The `models.go` file in the `taskapi` package establishes the fundamental data structures and persistence contract for a task management system. It defines domain models representing work items, team members, and organizational groups, while providing an abstraction layer for data storage operations.

## Domain Models

### Task

The `Task` struct represents a work item in the system with the following attributes:

- **ID**: Unique identifier for the task
- **Title**: Short name or heading for the task
- **Description**: Detailed explanation of the task requirements
- **Status**: Current state using predefined constants (open, in_progress, done, closed)
- **Priority**: Numerical priority level (1-4 scale)
- **AssigneeID**: Optional reference to the User responsible for the task
- **Labels**: Optional array of categorical tags
- **CreatedAt**: Timestamp of task creation
- **UpdatedAt**: Timestamp of last modification
- **DueDate**: Optional deadline for task completion

#### IsOverdue() Method

The `IsOverdue()` method returns `true` if a task has passed its due date and is not yet completed. A task is considered overdue only if:
- A due date has been set
- The current time is after the due date
- The status is neither "done" nor "closed"

### User

The `User` struct represents a team member who can be assigned tasks:

- **ID**: Unique identifier
- **Name**: Full name of the user
- **Email**: Contact email address
- **Role**: User's position or permission level
- **TeamID**: Reference to the user's team
- **CreatedAt**: Account creation timestamp

### Team

The `Team` struct groups users for organizational and task assignment purposes:

- **ID**: Unique identifier
- **Name**: Team name
- **Members**: Array of User IDs belonging to the team

## Status Constants

- `StatusOpen`: Task has been created but work has not begun
- `StatusInProgress`: Work is actively underway
- `StatusDone`: Work is complete
- `StatusClosed`: Task is archived or cancelled

## Priority Constants

- `PriorityLow`: 1 - Lowest urgency
- `PriorityMedium`: 2 - Standard urgency
- `PriorityHigh`: 3 - High urgency
- `PriorityCritical`: 4 - Highest urgency

## Persistence Interface

### TaskStore Interface

The `TaskStore` interface defines the contract for task persistence implementations:

- **ListTasks(status string)**: Retrieve all tasks filtered by status, returning a slice of Task pointers and an error
- **GetTask(id string)**: Retrieve a single task by its identifier
- **CreateTask(task *Task)**: Persist a new task to storage
- **UpdateTask(id string, updates map[string]any)**: Modify an existing task with partial updates
- **DeleteTask(id string)**: Remove a task from storage

This interface allows multiple storage backend implementations (database, file system, etc.) without coupling the domain logic to specific persistence mechanisms.

## Utility Functions

### generateID()

Generates a cryptographically random 8-byte hexadecimal identifier using the `crypto/rand` package. Used for creating unique IDs for entities.

## Dependencies

- `crypto/rand`: Secure random number generation for IDs
- `fmt`: String formatting for ID generation
- `time`: Timestamp handling and deadline calculations