---
{
  "title": "Task Management System Domain Models",
  "summary": "This module defines the core domain models for a task management system using Python dataclasses with type hints. It includes enumerations for task states, priority levels, and user permissions, along with three primary entities: Task, User, and Team, each with validation and serialization capabilities.",
  "concepts": [
    "dataclasses",
    "type hints",
    "enumerations",
    "Task",
    "User",
    "Team",
    "TaskStatus",
    "Priority",
    "UserRole",
    "UUID identification",
    "timestamp tracking",
    "serialization",
    "validation"
  ],
  "categories": [
    "domain models",
    "task management",
    "data structures",
    "Python dataclasses",
    "system architecture"
  ],
  "source_docs": [
    "655bf3f323830c87"
  ],
  "backlinks": null,
  "word_count": 460,
  "compiled_at": "2026-04-07T06:39:05Z",
  "compiled_with": "claude-haiku-4-5-20251001",
  "version": 1
}
---

# Task Management System Domain Models

## Overview

The `models.py` module provides the foundational data structures for a task management system. It leverages Python's `dataclasses` module combined with type hints to create clear, maintainable domain models. All models include automatic ID generation via UUID and timestamp tracking with UTC timezone awareness.

## Enumerations

### TaskStatus
Defines the possible lifecycle states for a task:
- **OPEN**: Task is newly created and awaiting action
- **IN_PROGRESS**: Task is actively being worked on
- **DONE**: Task work is completed
- **CLOSED**: Task is archived or dismissed

### Priority
Represents urgency levels for task execution:
- **LOW** (1): Non-urgent work items
- **MEDIUM** (2): Standard priority (default)
- **HIGH** (3): Important items requiring attention
- **CRITICAL** (4): Highest priority requiring immediate action

### UserRole
Defines permission levels within the system:
- **VIEWER**: Read-only access to tasks and team information
- **MEMBER**: Can view and manage assigned tasks
- **ADMIN**: Full control over team tasks and members
- **OWNER**: System owner with ultimate permissions

## Core Models

### Task
Represents a work item with comprehensive tracking capabilities.

**Attributes:**
- `title` (str): Required task name
- `description` (str): Optional detailed information
- `status` (TaskStatus): Current task state (default: OPEN)
- `priority` (Priority): Urgency level (default: MEDIUM)
- `assignee_id` (Optional[str]): User ID responsible for the task
- `labels` (list[str]): Custom tags for organization
- `id` (str): Auto-generated unique identifier
- `created_at` (datetime): UTC timestamp of creation
- `updated_at` (datetime): UTC timestamp of last modification
- `due_date` (Optional[datetime]): Target completion date

**Methods:**
- `is_overdue() -> bool`: Returns True if task has a due date in the past and is not completed or closed
- `to_dict() -> dict`: Converts task to a serializable dictionary with ISO format timestamps

### User
Represents a team member who can be assigned tasks.

**Attributes:**
- `name` (str): User's display name
- `email` (str): User's email address
- `role` (UserRole): Permission level (default: MEMBER)
- `team_id` (Optional[str]): Associated team identifier
- `id` (str): Auto-generated unique identifier
- `created_at` (datetime): UTC timestamp of account creation

### Team
Groups users together for task assignment and permission management.

**Attributes:**
- `name` (str): Team display name
- `members` (list[str]): Collection of user IDs in the team
- `id` (str): Auto-generated unique identifier

**Methods:**
- `add_member(user_id: str)`: Adds a user to the team if not already present
- `remove_member(user_id: str)`: Removes a user from the team

## Design Patterns

- **Dataclasses**: Provides automatic `__init__`, `__repr__`, and `__eq__` methods
- **Type Hints**: Full type annotation for IDE support and validation
- **Default Factories**: UUID generation and timestamp creation via callable defaults
- **Enum Types**: Type-safe status and permission representations
- **UTC Timezone**: All timestamps use UTC for consistency across systems
- **Serialization**: Task model includes `to_dict()` method for API responses