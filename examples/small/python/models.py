"""Domain models for the task management system.

Uses dataclasses with type hints for clear structure.
Includes validation, serialization, and enum types.
"""

from dataclasses import dataclass, field
from datetime import datetime, timezone
from enum import Enum
from typing import Optional
import uuid


class TaskStatus(str, Enum):
    """Possible states for a task."""
    OPEN = "open"
    IN_PROGRESS = "in_progress"
    DONE = "done"
    CLOSED = "closed"


class Priority(int, Enum):
    """Task priority levels."""
    LOW = 1
    MEDIUM = 2
    HIGH = 3
    CRITICAL = 4


class UserRole(str, Enum):
    """User permission levels."""
    VIEWER = "viewer"
    MEMBER = "member"
    ADMIN = "admin"
    OWNER = "owner"


@dataclass
class Task:
    """A work item that can be assigned, tracked, and completed."""
    title: str
    description: str = ""
    status: TaskStatus = TaskStatus.OPEN
    priority: Priority = Priority.MEDIUM
    assignee_id: Optional[str] = None
    labels: list[str] = field(default_factory=list)
    id: str = field(default_factory=lambda: uuid.uuid4().hex[:16])
    created_at: datetime = field(default_factory=lambda: datetime.now(timezone.utc))
    updated_at: datetime = field(default_factory=lambda: datetime.now(timezone.utc))
    due_date: Optional[datetime] = None

    def is_overdue(self) -> bool:
        """Check if the task is past its due date and still open."""
        if not self.due_date or self.status in (TaskStatus.DONE, TaskStatus.CLOSED):
            return False
        return datetime.now(timezone.utc) > self.due_date

    def to_dict(self) -> dict:
        """Serialize task to a dictionary."""
        return {
            "id": self.id,
            "title": self.title,
            "description": self.description,
            "status": self.status.value,
            "priority": self.priority.value,
            "assignee_id": self.assignee_id,
            "labels": self.labels,
            "created_at": self.created_at.isoformat(),
            "updated_at": self.updated_at.isoformat(),
            "due_date": self.due_date.isoformat() if self.due_date else None,
        }


@dataclass
class User:
    """A team member who can be assigned tasks."""
    name: str
    email: str
    role: UserRole = UserRole.MEMBER
    team_id: Optional[str] = None
    id: str = field(default_factory=lambda: uuid.uuid4().hex[:16])
    created_at: datetime = field(default_factory=lambda: datetime.now(timezone.utc))


@dataclass
class Team:
    """Groups users for task assignment and permissions."""
    name: str
    members: list[str] = field(default_factory=list)
    id: str = field(default_factory=lambda: uuid.uuid4().hex[:16])

    def add_member(self, user_id: str):
        if user_id not in self.members:
            self.members.append(user_id)

    def remove_member(self, user_id: str):
        self.members = [m for m in self.members if m != user_id]
