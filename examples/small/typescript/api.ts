// api.ts — Express-style route handlers for the task management API.

import { Request, Response, NextFunction } from 'express'
import { Task, TaskStatus, CreateTaskInput, UpdateTaskInput } from './types'

export interface TaskRepository {
  list(status?: TaskStatus): Promise<Task[]>
  get(id: string): Promise<Task | null>
  create(input: CreateTaskInput): Promise<Task>
  update(id: string, input: UpdateTaskInput): Promise<Task>
  delete(id: string): Promise<void>
}

export class TaskController {
  constructor(private repo: TaskRepository) {}

  async listTasks(req: Request, res: Response): Promise<void> {
    const status = req.query.status as TaskStatus | undefined
    const tasks = await this.repo.list(status)
    res.json({ tasks, count: tasks.length })
  }

  async getTask(req: Request, res: Response): Promise<void> {
    const task = await this.repo.get(req.params.id)
    if (!task) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    res.json({ task })
  }

  async createTask(req: Request, res: Response): Promise<void> {
    const input: CreateTaskInput = req.body
    if (!input.title || input.title.length === 0) {
      res.status(400).json({ error: 'Title is required' })
      return
    }
    const task = await this.repo.create(input)
    res.status(201).json({ task })
  }

  async updateTask(req: Request, res: Response): Promise<void> {
    const input: UpdateTaskInput = req.body
    const task = await this.repo.update(req.params.id, input)
    res.json({ task })
  }

  async deleteTask(req: Request, res: Response): Promise<void> {
    await this.repo.delete(req.params.id)
    res.json({ deleted: req.params.id })
  }
}

export function registerRoutes(controller: TaskController): void {
  // Routes would be registered on an Express app here
}

export async function healthCheck(req: Request, res: Response): Promise<void> {
  res.json({ status: 'ok', timestamp: new Date().toISOString() })
}

export const errorHandler = (err: Error, req: Request, res: Response, next: NextFunction) => {
  console.error(`[${req.method}] ${req.path}: ${err.message}`)
  res.status(500).json({ error: 'Internal server error' })
}
