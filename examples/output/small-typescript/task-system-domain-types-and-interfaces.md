---
{
  "title": "Task System Domain Types and Interfaces",
  "summary": "Comprehensive TypeScript type definitions for a task management system, including domain entities (Task, User, Team), input interfaces for mutations, and enums for status, priority, and user role management. Defines the core data structures and contracts for task operations, filtering, and sorting functionality.",
  "concepts": [
    "Task",
    "User",
    "Team",
    "TaskStatus",
    "Priority",
    "UserRole",
    "CreateTaskInput",
    "UpdateTaskInput",
    "TaskFilter",
    "SortField",
    "SortOrder",
    "PaginatedResponse",
    "Domain Types",
    "Type Contracts",
    "Input Validation",
    "Workflow States",
    "Access Control"
  ],
  "categories": [
    "TypeScript",
    "Type Definitions",
    "Domain Models",
    "Task Management",
    "API Contracts",
    "Data Structures",
    "Enumerations"
  ],
  "source_docs": [
    "0053b943ea24f4c5"
  ],
  "backlinks": null,
  "word_count": 553,
  "compiled_at": "2026-04-07T06:39:17Z",
  "compiled_with": "claude-haiku-4-5-20251001",
  "version": 1
}
---

# Task System Domain Types

## Overview
This module (`types.ts`) exports the foundational type definitions, interfaces, and enumerations that govern the task management system. It establishes contracts for data structures, input validation, and domain state.

## Enumerations

### TaskStatus
Represents the workflow states of a task:
- `Open` - Initial state, not yet started
- `InProgress` - Currently being worked on
- `Done` - Completed work
- `Closed` - Finalized and archived

### Priority
Numerical scale indicating task urgency:
- `Low` (1) - Non-urgent
- `Medium` (2) - Standard importance
- `High` (3) - Important
- `Critical` (4) - Blocking/urgent

### UserRole
Access control levels within teams:
- `Viewer` - Read-only access
- `Member` - Standard contributor access
- `Admin` - Administrative privileges
- `Owner` - Full ownership and control

## Core Domain Interfaces

### Task
Represents a single task entity with metadata:
- `id` - Unique identifier
- `title` - Task name (required)
- `description` - Detailed information
- `status` - Current workflow state (TaskStatus)
- `priority` - Urgency level (Priority)
- `assigneeId` - Optional user assignment
- `labels` - Array of categorical tags
- `createdAt` - Creation timestamp
- `updatedAt` - Last modification timestamp
- `dueDate` - Optional deadline

### User
Represents a user account:
- `id` - Unique identifier
- `name` - Display name
- `email` - Contact email
- `role` - Permission level (UserRole)
- `teamId` - Optional team association
- `createdAt` - Account creation timestamp

### Team
Represents a group or organization:
- `id` - Unique identifier
- `name` - Team name
- `members` - Array of user IDs

## Input Interfaces

### CreateTaskInput
Contract for creating new tasks (all fields optional except title):
- `title` - Required task name
- `description` - Optional details
- `priority` - Optional importance level
- `assigneeId` - Optional user assignment
- `labels` - Optional categorical tags
- `dueDate` - Optional deadline

### UpdateTaskInput
Contract for modifying existing tasks (all fields optional):
- `title` - Update task name
- `description` - Update details
- `status` - Transition workflow state
- `priority` - Adjust importance
- `assigneeId` - Reassign user (null clears assignment)
- `labels` - Update tags
- `dueDate` - Update deadline (null clears deadline)

## Filtering and Sorting

### TaskFilter
Type for query filters:
```typescript
{
  status?: TaskStatus      // Filter by workflow state
  priority?: Priority      // Filter by urgency level
  assigneeId?: string      // Filter by assigned user
  label?: string          // Filter by single tag
}
```

### SortField
Available fields for sorting results:
- `created_at` - Sort by creation timestamp
- `updated_at` - Sort by modification timestamp
- `priority` - Sort by urgency level
- `due_date` - Sort by deadline

### SortOrder
Sort direction:
- `asc` - Ascending order
- `desc` - Descending order

## Pagination

### PaginatedResponse<T>
Generic container for paginated results:
- `items` - Array of results (generic type T)
- `page` - Current page number
- `perPage` - Items per page
- `total` - Total number of items
- `pages` - Total number of pages

## Design Patterns

1. **Partial Updates** - UpdateTaskInput uses optional fields and nullable types for null-clearing assignments and dates
2. **Type Safety** - Enumerations ensure only valid states are assigned
3. **Flexibility** - Nullable ID fields (`assigneeId | null`) allow unassignment operations
4. **Timestamp Tracking** - All entities include creation/modification timestamps for auditing