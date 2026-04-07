---
{
  "title": "Task Management System Utility Functions",
  "summary": "A collection of standalone helper functions for string processing, validation, date formatting, and pagination in a task management system. Provides utilities for slugification, email validation, date formatting with multiple styles, relative time display, sequence pagination, and text truncation.",
  "concepts": [
    "string slugification",
    "email validation",
    "datetime formatting",
    "relative time representation",
    "sequence pagination",
    "text truncation",
    "regex patterns",
    "URL-friendly strings",
    "type hints",
    "utility functions"
  ],
  "categories": [
    "string processing",
    "data validation",
    "date/time formatting",
    "pagination",
    "utilities",
    "helpers",
    "task management"
  ],
  "source_docs": [
    "0c35a503352404f6"
  ],
  "backlinks": null,
  "word_count": 408,
  "compiled_at": "2026-04-07T06:39:07Z",
  "compiled_with": "claude-haiku-4-5-20251001",
  "version": 1
}
---

# Task Management System Utilities

## Overview

The `utils.py` module provides essential utility functions for the task management system. These standalone helpers are designed to handle common operations across string processing, validation, date/time formatting, and data pagination.

## Module Constants

The module defines several regex patterns and configuration constants:

- **EMAIL_PATTERN**: Regex pattern for validating email addresses (`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
- **SLUG_STRIP**: Pattern to remove non-alphanumeric characters from slugs
- **SLUG_COLLAPSE**: Pattern to collapse multiple whitespace and hyphens
- **MAX_SLUG_LENGTH**: Maximum slug length set to 80 characters

## Functions

### String Processing

#### slugify(text: str) -> str
Converts a string to a URL-friendly slug by:
1. Converting to lowercase and stripping whitespace
2. Removing non-alphanumeric characters (except hyphens)
3. Collapsing consecutive whitespace/hyphens into single hyphens
4. Limiting to MAX_SLUG_LENGTH (80 characters)

#### truncate(text: str, max_length: int = 200, suffix: str = "...") -> str
Truncates text to a maximum length while preserving word integrity by appending a suffix. Returns original text if already within max_length.

### Validation

#### validate_email(email: str) -> bool
Performs syntactic validation of email addresses using the EMAIL_PATTERN regex. Returns True if the email format is valid, False otherwise.

### Date and Time Formatting

#### format_date(dt: datetime, style: str = "short") -> str
Formats datetime objects in multiple styles:
- **"iso"**: ISO 8601 format (e.g., "2024-01-15T10:30:00")
- **"human"**: Human-readable format (e.g., "January 15, 2024 at 10:30 AM")
- **"short"** (default): Compact format (e.g., "2024-01-15 10:30")

#### time_ago(dt: datetime) -> str
Returns a human-readable relative time string showing how long ago an event occurred:
- Less than 60 seconds: "just now"
- Less than 1 hour: "{minutes}m ago"
- Less than 24 hours: "{hours}h ago"
- 24+ hours: "{days}d ago"

Uses UTC timezone for accurate comparisons.

### Data Pagination

#### paginate(items: Sequence[T], page: int = 1, per_page: int = 20) -> dict
Paginates a sequence of items and returns a dictionary containing:
- **items**: Sliced list of items for the current page
- **page**: Current page number
- **per_page**: Items per page (default: 20)
- **total**: Total number of items
- **pages**: Total number of pages

Supports generic type T for flexible item handling.

## Dependencies

- **re**: Regular expression operations
- **datetime**: Date and time handling
- **typing**: Type hints (TypeVar, Sequence)

## Usage Patterns

These utilities are designed as standalone functions, making them reusable across the task management system without dependencies on other modules. They follow consistent naming conventions and include sensible defaults for optional parameters.