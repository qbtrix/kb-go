---
{
  "title": "Task API HTTP Handler Implementation",
  "summary": "The handler.go module provides HTTP request handlers for complete CRUD operations on tasks, implementing endpoints for listing, creating, retrieving, updating, and deleting tasks. It uses a dependency-injected store interface and standardizes JSON request/response serialization across all endpoints.",
  "concepts": [
    "HTTP handlers",
    "CRUD operations",
    "dependency injection",
    "JSON serialization",
    "RESTful API",
    "request validation",
    "error handling",
    "path parameters",
    "query parameters",
    "HTTP status codes"
  ],
  "categories": [
    "API Design",
    "HTTP Handlers",
    "Go Web Development",
    "Request/Response Processing",
    "Software Architecture"
  ],
  "source_docs": [
    "366049064f88dda2"
  ],
  "backlinks": null,
  "word_count": 486,
  "compiled_at": "2026-04-07T06:38:57Z",
  "compiled_with": "claude-haiku-4-5-20251001",
  "version": 1
}
---

# Task API HTTP Handler Implementation

## Overview

The `handler.go` module in the `taskapi` package implements HTTP handlers for task management CRUD operations. It provides a clean, RESTful interface for task operations with standardized error handling and JSON serialization.

## Architecture

### TaskHandler Structure

The `TaskHandler` struct serves as the central handler component, holding a reference to a `TaskStore` dependency for data persistence:

```go
type TaskHandler struct {
    store TaskStore
}
```

This design follows dependency injection principles, allowing flexible storage backend implementations.

### Factory Function

`NewTaskHandler(store TaskStore)` creates and returns a new TaskHandler instance initialized with the provided store.

## Request/Response Types

### CreateRequest

Defines the payload structure for task creation:
- **Title** (string): Task title
- **Description** (string): Detailed task description
- **AssigneeID** (string, optional): ID of assigned user
- **Priority** (int): Priority level

### TaskResponse

Wraps task data for API output with optional error information:
- **Task** (*Task): The task object
- **Error** (string, optional): Error message if operation failed

## HTTP Endpoints

### List Handler
Retrieves all tasks with optional status filtering via query parameter.
- **HTTP Method**: GET
- **Query Parameters**: `status` (optional filter)
- **Response**: JSON object containing task array and count
- **Error Handling**: Returns 500 Internal Server Error on store failure

### Create Handler
Adds a new task to the system.
- **HTTP Method**: POST
- **Request Body**: CreateRequest JSON payload
- **Response**: TaskResponse with created task (HTTP 201 Created)
- **Error Handling**: Returns 400 Bad Request for invalid body, 500 for store errors
- **Behavior**: Generates unique ID, sets status to "open", timestamps creation

### Get Handler
Retrrieves a single task by ID.
- **HTTP Method**: GET
- **Path Parameter**: `id`
- **Response**: TaskResponse with task data
- **Error Handling**: Returns 404 Not Found if task doesn't exist

### Update Handler
Modifies an existing task with provided field updates.
- **HTTP Method**: PATCH/PUT
- **Path Parameter**: `id`
- **Request Body**: Map of fields to update
- **Response**: TaskResponse with updated task
- **Error Handling**: Returns 400 for invalid body, 404 if task not found

### Delete Handler
Removes a task from the system.
- **HTTP Method**: DELETE
- **Path Parameter**: `id`
- **Response**: JSON confirmation with deleted task ID
- **Error Handling**: Returns 404 Not Found if task doesn't exist

## Utility Functions

### writeJSON
Private helper function that:
- Sets Content-Type header to application/json
- Writes HTTP status code
- Encodes and sends the provided value as JSON

### writeError
Private helper function that standardizes error responses by calling `writeJSON` with an error message map.

## Dependencies

- **encoding/json**: JSON marshaling/unmarshaling
- **net/http**: HTTP request/response types and utilities
- **time**: Timestamp generation for task creation/updates

## Design Patterns

- **Dependency Injection**: Store interface injected via constructor
- **Receiver Methods**: HTTP handlers implemented as methods on TaskHandler
- **Standardized Responses**: Consistent JSON response format across endpoints
- **Error Wrapping**: TaskResponse structure allows error inclusion with task data