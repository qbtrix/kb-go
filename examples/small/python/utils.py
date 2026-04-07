"""Utility functions for the task management system.

Standalone helpers for string processing, validation,
date formatting, and pagination.
"""

import re
from datetime import datetime, timezone
from typing import TypeVar, Sequence

T = TypeVar("T")

EMAIL_PATTERN = re.compile(r"^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$")
SLUG_STRIP = re.compile(r"[^a-z0-9\s-]")
SLUG_COLLAPSE = re.compile(r"[\s-]+")

MAX_SLUG_LENGTH = 80


def slugify(text: str) -> str:
    """Convert a string to a URL-friendly slug."""
    lower = text.lower().strip()
    clean = SLUG_STRIP.sub("", lower)
    slug = SLUG_COLLAPSE.sub("-", clean).strip("-")
    return slug[:MAX_SLUG_LENGTH]


def validate_email(email: str) -> bool:
    """Check if an email address is syntactically valid."""
    return bool(EMAIL_PATTERN.match(email))


def format_date(dt: datetime, style: str = "short") -> str:
    """Format a datetime for display."""
    if style == "iso":
        return dt.isoformat()
    elif style == "human":
        return dt.strftime("%B %d, %Y at %I:%M %p")
    else:
        return dt.strftime("%Y-%m-%d %H:%M")


def time_ago(dt: datetime) -> str:
    """Return a human-readable relative time string."""
    now = datetime.now(timezone.utc)
    delta = now - dt
    seconds = int(delta.total_seconds())

    if seconds < 60:
        return "just now"
    elif seconds < 3600:
        minutes = seconds // 60
        return f"{minutes}m ago"
    elif seconds < 86400:
        hours = seconds // 3600
        return f"{hours}h ago"
    else:
        days = seconds // 86400
        return f"{days}d ago"


def paginate(items: Sequence[T], page: int = 1, per_page: int = 20) -> dict:
    """Paginate a sequence of items."""
    total = len(items)
    start = (page - 1) * per_page
    end = start + per_page
    return {
        "items": list(items[start:end]),
        "page": page,
        "per_page": per_page,
        "total": total,
        "pages": (total + per_page - 1) // per_page,
    }


def truncate(text: str, max_length: int = 200, suffix: str = "...") -> str:
    """Truncate text to a maximum length with a suffix."""
    if len(text) <= max_length:
        return text
    return text[: max_length - len(suffix)] + suffix
