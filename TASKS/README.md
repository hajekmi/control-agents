# Task workflow

Directories:

- `backlog/`: ready tasks
- `in-progress/`: the single active task
- `done/`: completed and validated tasks

Task names use:

`NNNN-short-description.md`

Each task must be self-contained and define its scope, requirements, material decisions, references, and acceptance criteria without relying on conversation history.

When processing the backlog, handle tasks sequentially in filename order and stop on the first blocker.

If a task exists in `in-progress/`, resume or resolve it before selecting another backlog task.

Workflow:

1. Read the complete task and referenced documents.
2. Move it from `backlog/` to `in-progress/` before implementation.
3. Implement, test, review, and verify every acceptance criterion.
4. Record a concise implementation and validation summary in the task.
5. Move it to `done/` only when no blocking issue remains.

If blocked, keep the task in `in-progress/` and document the exact blocker.
