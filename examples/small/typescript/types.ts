// types.ts — Domain types, interfaces, and enums for the task system.

export enum TaskStatus {
  Open = 'open',
  InProgress = 'in_progress',
  Done = 'done',
  Closed = 'closed',
}

export enum Priority {
  Low = 1,
  Medium = 2,
  High = 3,
  Critical = 4,
}

export enum UserRole {
  Viewer = 'viewer',
  Member = 'member',
  Admin = 'admin',
  Owner = 'owner',
}

export interface Task {
  id: string
  title: string
  description: string
  status: TaskStatus
  priority: Priority
  assigneeId?: string
  labels: string[]
  createdAt: Date
  updatedAt: Date
  dueDate?: Date
}

export interface User {
  id: string
  name: string
  email: string
  role: UserRole
  teamId?: string
  createdAt: Date
}

export interface Team {
  id: string
  name: string
  members: string[]
}

export interface CreateTaskInput {
  title: string
  description?: string
  priority?: Priority
  assigneeId?: string
  labels?: string[]
  dueDate?: Date
}

export interface UpdateTaskInput {
  title?: string
  description?: string
  status?: TaskStatus
  priority?: Priority
  assigneeId?: string | null
  labels?: string[]
  dueDate?: Date | null
}

export interface PaginatedResponse<T> {
  items: T[]
  page: number
  perPage: number
  total: number
  pages: number
}

export type TaskFilter = {
  status?: TaskStatus
  priority?: Priority
  assigneeId?: string
  label?: string
}

export type SortField = 'created_at' | 'updated_at' | 'priority' | 'due_date'
export type SortOrder = 'asc' | 'desc'
