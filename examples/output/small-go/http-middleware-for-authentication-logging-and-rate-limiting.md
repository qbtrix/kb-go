---
{
  "title": "HTTP Middleware for Authentication, Logging, and Rate Limiting",
  "summary": "The middleware.go module provides three HTTP middleware components for the taskapi package: authentication validation via API tokens, request logging with timing information, and rate limiting based on sliding window per IP address. These middlewares enable secure, observable, and throttled API access.",
  "concepts": [
    "HTTP Middleware",
    "Authentication",
    "API Token Validation",
    "Request Logging",
    "Rate Limiting",
    "Sliding Window Algorithm",
    "IP-based Throttling",
    "Thread Safety",
    "Mutex Locking",
    "Request Duration Measurement"
  ],
  "categories": [
    "Web Development",
    "API Security",
    "Go Programming",
    "HTTP Middleware",
    "Request Processing",
    "Rate Limiting",
    "Observability"
  ],
  "source_docs": [
    "557d913ffba8bde8"
  ],
  "backlinks": null,
  "word_count": 331,
  "compiled_at": "2026-04-07T06:38:55Z",
  "compiled_with": "claude-haiku-4-5-20251001",
  "version": 1
}
---

# HTTP Middleware for taskapi

## Overview

The `middleware.go` module implements three critical HTTP middleware functions for the taskapi package, providing cross-cutting concerns for security, observability, and traffic management.

## Components

### AuthMiddleware

**Purpose**: Validates API tokens on incoming requests.

**Behavior**:
- Exempts `/health` endpoint from authentication
- Requires `Authorization` header on protected routes
- Rejects requests with missing authorization headers (401 Unauthorized)
- Rejects requests with invalid tokens (403 Forbidden)
- Forwards valid requests to the next handler

**Implementation**: Middleware wrapper that intercepts HTTP requests and performs token validation before passing control to downstream handlers.

### LoggingMiddleware

**Purpose**: Logs HTTP request metadata and execution duration.

**Behavior**:
- Records request HTTP method (GET, POST, etc.)
- Records request URL path
- Measures and logs request processing duration
- Uses standard Go logging to output metrics

**Implementation**: Wraps the next handler, recording start time before execution and calculating elapsed time after completion.

### RateLimiter

**Purpose**: Enforces rate limiting per client IP using a sliding window algorithm.

**Structure**:
- `mu`: Mutex for thread-safe concurrent access
- `requests`: Map tracking timestamp lists per IP address
- `limit`: Maximum allowed requests per window
- `window`: Time duration defining the rate limit period

**Behavior**:
- Maintains a map of request timestamps per IP address
- Implements sliding window by removing timestamps older than the window duration
- Allows requests if the count within the window is below the limit
- Returns `false` when rate limit is exceeded

**Thread Safety**: Protected by mutex locks to safely handle concurrent requests.

## Helper Functions

### validateToken(token)

Validates API tokens by checking minimum length requirement (>10 characters). Can be extended with cryptographic validation or database lookups.

## Usage Example

```go
// Create rate limiter: 100 requests per 1 minute per IP
limiter := NewRateLimiter(100, time.Minute)

// Chain middlewares in HTTP handler
http.Handle("/api/tasks", 
  LoggingMiddleware(
    AuthMiddleware(
      rateLimitMiddleware(limiter, handler))))
```

## Dependencies

- `log`: Standard logging
- `net/http`: HTTP handler interfaces
- `sync`: Mutex for concurrency control
- `time`: Timestamp and duration operations
