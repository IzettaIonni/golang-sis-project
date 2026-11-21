# Golang SIS project - Text Analyzer

KBTU, Golang Application Development, Fall 2026. Checkpoint 1 + Checkpoint 2.

## What it does

The program takes some text and counts:

- words
- unique words
- characters
- sentences
- the most common word

Every text is a task. Tasks are stored in memory and processed by several worker goroutines at the same time.

Example: `"The cat sat. The cat ran!"` gives 6 words, 4 unique, 25 characters, 2 sentences, most common word `cat`.

Empty text (or only spaces) is not accepted. Text without any words, like `"?!..."`, is accepted, but processing fails and the task becomes `failed`.

## How to run

```
go run .
go run . "Hello Go world" "Some other text."
```

Tests:

```
go test ./...
go test -race ./...
```

## Structure

```
main.go      - demo
analyzer/    - text validation and analysis
task/        - Task struct and Store (in-memory storage)
worker/      - worker pool
```

## How a task goes through the program

1. `store.Create(text)` checks the text and saves the task as `pending`.
2. `pool.Submit(id)` marks it `queued` and puts the id into the queue. It doesn't wait for the result.
3. A free worker takes the id, marks the task `processing` and analyzes the text.
4. The worker saves the result and the task becomes `completed` (or `failed`).

Statuses:

```
pending -> queued -> processing -> completed
                                -> failed

queued -> pending   (only when the queue was full)
```

`completed` and `failed` are final. Submitting a task that is not `pending` returns an error.

## Worker pool

- 3 workers and queue size 10 (set in `main.go`)
- the queue is a buffered channel `chan int` with task ids
- workers are started once, each one runs `for id := range queue`, so it keeps taking new tasks
- if the queue is full, `Submit` returns `ErrQueueFull` right away (it doesn't block) and the task goes back to `pending`, so it can be submitted later

```
Submit(id) -> [ queue ] -> worker 1 \
                        -> worker 2  -> Store (results)
                        -> worker 3 /
```

## Synchronization

- The store keeps tasks in a map protected by `sync.RWMutex`. `Get` and `List` use `RLock`, changes use `Lock`.
- Tasks are stored as values, not pointers, so `Get` returns a copy. Nobody can change a task without the lock.
- Each status change checks the old status and sets the new one under the same lock, so the same task can't be queued or processed twice.
- The lock is not held while the text is being analyzed.
- The pool has its own `sync.Mutex` for the `stopped` flag. `Submit` (send to channel) and `Stop` (close channel) both take it, so they never run at the same time and there is no send on a closed channel.

## Stopping

`pool.Stop()`:

1. sets `stopped = true` and closes the queue
2. waits for all workers with `sync.WaitGroup`

Workers finish everything that is already in the queue and then exit, so accepted tasks are not lost. After `Stop`, `Submit` returns `ErrPoolStopped`. Calling `Stop` twice is fine.

## Tests

Tests cover the analyzer, the store and the pool: results, statuses, invalid transitions, full queue, duplicate submits, stop, submit after stop and concurrent access.

For concurrency tests I use a fake processor that waits on a channel. This way I can check that two tasks are really processed at the same time without `time.Sleep`.
