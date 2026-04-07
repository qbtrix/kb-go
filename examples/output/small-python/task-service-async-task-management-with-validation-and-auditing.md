---
{
  "title": "Task Service — Async Task Management with Validation and Auditing",
  "summary": "The Task Service module provides core business logic for task lifecycle management through async CRUD operations with built-in validation, audit logging, and state transition rules. It implements a repository pattern for persistence abstraction and enforces constraints on task creation, assignment, and status transitions.",
  "concepts": [
    "task management",
    "CRUD operations",
    "state machine",
    "async programming",
    "repository pattern",
    "validation",
    "audit logging",
    "business logic",
    "status transitions",
    "task lifecycle"
  ],
  "categories": [
    "backend services",
    "task management systems",
    "async patterns",
    "data persistence",
    "business logic layer"
  ],
  "source_docs": [
    "9464c550ba0e1a48"
  ],
  "backlinks": null,
  "word_count": 414,
  "compiled_at": "2026-04-07T06:39:06Z",
  "compiled_with": "claude-haiku-4-5-20251001",
  "version": 1
}
---

# Task Service Module

## Overview

The `service.py` module contains the **TaskService** class, which manages the complete lifecycle of tasks in the system. It provides async operations for creating, assigning, transitioning, and querying tasks with comprehensive validation and audit logging.

## Architecture

### BaseService

A shared base class that provides common functionality to all service classes:

- **Repository Pattern**: Accepts a repository instance for data persistence abstraction
- **Logging**: Optional logger integration for audit trails via `_log()` method
- **Method**: `_log(action, entity_id)` — Logs service actions with entity identifiers

### TaskService

Extends `BaseService` to manage task operations with the following responsibilities:

#### Configuration Constants
- `MAX_TITLE_LENGTH = 200` — Enforces title length constraints
- `MAX_DESCRIPTION_LENGTH = 5000` — Enforces description length constraints

#### Core Operations

**Create Task** (`async create_task()`)
- Validates title length before creation
- Initializes task with status `OPEN` and current UTC timestamp
- Defaults priority to `MEDIUM` if not specified
- Optionally assigns an assignee on creation
- Persists via repository and logs action

**Assign Task** (`async assign_task()`)
- Retrieves task from repository
- Validates task existence; raises `KeyError` if not found
- Updates assignee and modification timestamp
- Logs assignment with source and target user IDs

**Transition Status** (`async transition()`)
- Implements state machine validation using `TRANSITIONS` mapping
- Prevents invalid status transitions
- Updates task status and timestamp on successful transition
- Raises `ValueError` for invalid state transitions

**List Tasks** (`async list_tasks()`)
- Queries repository with optional status filtering
- Supports pagination via `limit` parameter (default: 50)
- Returns filtered task list

**Get Overdue Tasks** (`async get_overdue()`)
- Queries all open tasks
- Filters for tasks with due dates in the past (compared to UTC now)
- Returns overdue task list

## State Machine

The `TRANSITIONS` constant defines valid status transitions:

```
OPEN → [IN_PROGRESS, CLOSED]
IN_PROGRESS → [DONE, OPEN]
DONE → [CLOSED, OPEN]
CLOSED → [OPEN]
```

This enforces a controlled workflow where tasks can move forward through completion stages or roll back to earlier states.

## Key Design Patterns

- **Async/Await**: All I/O operations are non-blocking
- **Repository Pattern**: Decouples business logic from persistence
- **Validation**: Input validation at service layer before persistence
- **Audit Logging**: All mutations logged via `_log()` for compliance
- **State Machine**: Explicit transition rules prevent invalid state changes
- **Timestamps**: UTC-aware timestamps for all temporal data

## Dependencies

- `datetime`, `timezone` — UTC timestamp generation
- `typing` — Type hints for static analysis
- `models` — Task, User, TaskStatus, Priority domain models