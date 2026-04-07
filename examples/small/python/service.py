"""Task service — core business logic for task management.

Provides async CRUD operations with validation, notifications,
and audit logging. Delegates persistence to a TaskRepository.
"""

from datetime import datetime, timezone
from typing import Optional

from models import Task, User, TaskStatus, Priority


class BaseService:
    """Base class with shared utilities for all services."""

    def __init__(self, repository, logger=None):
        self.repository = repository
        self.logger = logger

    def _log(self, action: str, entity_id: str):
        if self.logger:
            self.logger.info(f"{action}: {entity_id}")


class TaskService(BaseService):
    """Manages task lifecycle — create, assign, transition, close."""

    MAX_TITLE_LENGTH = 200
    MAX_DESCRIPTION_LENGTH = 5000

    async def create_task(
        self, title: str, description: str, priority: Priority = Priority.MEDIUM,
        assignee_id: Optional[str] = None
    ) -> Task:
        """Create a new task with validation."""
        if len(title) > self.MAX_TITLE_LENGTH:
            raise ValueError(f"Title exceeds {self.MAX_TITLE_LENGTH} chars")

        task = Task(
            title=title,
            description=description,
            status=TaskStatus.OPEN,
            priority=priority,
            assignee_id=assignee_id,
            created_at=datetime.now(timezone.utc),
        )
        await self.repository.save(task)
        self._log("created", task.id)
        return task

    async def assign_task(self, task_id: str, user: User) -> Task:
        """Assign a task to a user."""
        task = await self.repository.get(task_id)
        if task is None:
            raise KeyError(f"Task {task_id} not found")
        task.assignee_id = user.id
        task.updated_at = datetime.now(timezone.utc)
        await self.repository.save(task)
        self._log("assigned", f"{task_id} -> {user.id}")
        return task

    async def transition(self, task_id: str, new_status: TaskStatus) -> Task:
        """Move a task to a new status with validation."""
        task = await self.repository.get(task_id)
        if not task:
            raise KeyError(f"Task {task_id} not found")

        allowed = TRANSITIONS.get(task.status, [])
        if new_status not in allowed:
            raise ValueError(f"Cannot transition from {task.status} to {new_status}")

        task.status = new_status
        task.updated_at = datetime.now(timezone.utc)
        await self.repository.save(task)
        self._log("transitioned", f"{task_id}: {new_status}")
        return task

    async def list_tasks(self, status: Optional[str] = None, limit: int = 50) -> list[Task]:
        """List tasks with optional status filter."""
        return await self.repository.list(status=status, limit=limit)

    async def get_overdue(self) -> list[Task]:
        """Get all tasks that are past their due date."""
        tasks = await self.repository.list(status=TaskStatus.OPEN)
        now = datetime.now(timezone.utc)
        return [t for t in tasks if t.due_date and t.due_date < now]


# Valid status transitions
TRANSITIONS = {
    TaskStatus.OPEN: [TaskStatus.IN_PROGRESS, TaskStatus.CLOSED],
    TaskStatus.IN_PROGRESS: [TaskStatus.DONE, TaskStatus.OPEN],
    TaskStatus.DONE: [TaskStatus.CLOSED, TaskStatus.OPEN],
    TaskStatus.CLOSED: [TaskStatus.OPEN],
}
