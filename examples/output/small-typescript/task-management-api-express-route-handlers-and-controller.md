---
{
  "title": "Task Management API - Express Route Handlers and Controller",
  "summary": "This module defines the API layer for a task management system using Express.js, including a TaskController class that handles CRUD operations and supporting interfaces for repository abstraction. It provides route handlers for listing, retrieving, creating, updating, and deleting tasks, along with middleware for health checks and centralized error handling.",
  "concepts": [
    "TaskController",
    "TaskRepository",
    "CRUD operations",
    "Dependency injection",
    "Express middleware",
    "Error handling",
    "HTTP status codes",
    "REST API",
    "Type safety",
    "Interface abstraction",
    "Request validation",
    "Health check endpoint"
  ],
  "categories": [
    "API Development",
    "TypeScript",
    "Express.js",
    "Backend Architecture",
    "Data Access Patterns"
  ],
  "source_docs": [
    "bab09ad6f2fa710a"
  ],
  "backlinks": null,
  "word_count": 325,
  "compiled_at": "2026-04-07T06:39:15Z",
  "compiled_with": "claude-haiku-4-5-20251001",
  "version": 1
}
---

# Task Management API

## Overview
The `api.ts` module implements Express-style route handlers for a task management REST API. It follows a controller-based architecture with dependency injection via a repository interface, enabling flexible data persistence implementations.

## Core Components

### TaskRepository Interface
Defines the contract for data access operations:
- `list(status?: TaskStatus): Promise<Task[]>` - Retrieve all tasks, optionally filtered by status
- `get(id: string): Promise<Task | null>` - Fetch a single task by ID
- `create(input: CreateTaskInput): Promise<Task>` - Create a new task
- `update(id: string, input: UpdateTaskInput): Promise<Task>` - Modify an existing task
- `delete(id: string): Promise<void>` - Remove a task

### TaskController Class
Handles HTTP request processing for task operations:

#### listTasks()
Extracts optional status filter from query parameters and returns all matching tasks with count metadata.

#### getTask()
Retrrieves a single task by ID from the request path. Returns 404 status with error message if task not found.

#### createTask()
Validates that request body contains a non-empty title, returns 400 if invalid. Creates task via repository and responds with 201 Created status.

#### updateTask()
Processes task updates using ID from path parameters and merged properties from request body.

#### deleteTask()
Removes a task and confirms deletion by echoing the deleted task ID.

## Utility Functions

### registerRoutes(controller: TaskController)
Stub function intended to register HTTP route bindings to the provided controller instance on an Express application.

### healthCheck()
Endpoint that returns service status and current UTC timestamp for monitoring and availability checks.

### errorHandler()
Express error middleware that logs request context and error details, then returns standardized 500 error response to prevent leaking sensitive information to clients.

## Request/Response Patterns
All responses use JSON format. Success responses wrap data in consistent objects (e.g., `{ task }`, `{ tasks }`). Error responses include descriptive `error` field with appropriate HTTP status codes.

## TypeScript Integration
Full type safety through imported types (Task, TaskStatus, CreateTaskInput, UpdateTaskInput) and Express type definitions, ensuring compile-time validation of request/response handling.