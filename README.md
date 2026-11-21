# SIS Project: Text Analyzer

Golang Application Development, KBTU, Fall 2026. Checkpoints 1 and 2.

The application analyzes pieces of text. Each piece of text is a **task**. A task is
created, validated, put into a queue and then analyzed by one of several worker
goroutines. Everything is stored in memory.

## What a task does

Input: any text.

Result:

| Field            | Meaning                                                    |
|------------------|------------------------------------------------------------|
| `Words`          | number of words                                            |
| `UniqueWords`    | number of different words (case-insensitive)               |
| `Characters`     | number of characters (runes, so Cyrillic is counted right) |
| `Sentences`      | number of parts separated by `.`, `!` or `?` that contain a word |
| `MostCommonWord` | most frequent word, on a tie the alphabetically first one  |

Example: `"The cat sat. The cat ran!"` gives 6 words, 4 unique, 25 characters,
2 sentences, most common word `cat`.

A word is a sequence of letters, digits and apostrophes (`don't` is one word).

**Validation:** empty or whitespace-only text is rejected when the task is created
(`text is empty`).

**Processing error:** text with no words at all, for example `"?!..."`, passes
validation but analysis fails with `text contains no words`, so the task becomes `failed`.

## Project structure

```
main.go               demo: creates tasks, submits them, stops the pool, prints results
analyzer/analyzer.go  validation and text analysis (the domain logic)
task/task.go          Task struct, statuses, errors
task/store.go         in-memory storage of tasks, all status changes
worker/pool.go        worker pool: queue, workers, Submit, Stop
```

## How it works

```
            Submit(id)
 Store  ──────────────►  queue (chan int, capacity 10)
 (map +                    │        │        │
 RWMutex)                  ▼        ▼        ▼
   ▲                   worker 1  worker 2  worker 3
   │                       │        │        │
   └──── MarkProcessing / MarkCompleted / MarkFailed ────┘
```

1. `store.Create(text)` validates the text, gives it the next id and saves it as `pending`.
2. `pool.Submit(id)` marks the task `queued` and puts its **id** into the queue.
   It returns immediately and does not wait for processing.
3. A free worker takes the id from the queue, marks the task `processing`,
   reads its text and runs the analyzer.
4. The worker saves the result and marks the task `completed`
   (or `failed` with the error message).
5. `store.Get(id)` and `store.List()` can be called at any time, also while workers are busy.

### Worker pool

- **3 workers** and a **queue of 10** in `main.go` (both are parameters of `worker.NewPool`).
- Workers are started once in `NewPool`. Each worker is a loop `for id := range queue`,
  so it keeps taking new tasks until the queue is closed.
- Only a fixed number of goroutines exists, no goroutine is created per task.
- The worker gets its processor through the `Processor` interface. The real program uses
  `analyzer.TextAnalyzer`, the tests use a fake one that can block on purpose.

### When the queue is full

`Submit` does **not** wait. It tries to send with `select` + `default`. If the
channel is full, it returns `ErrQueueFull` and the task is put back to `pending`,
so it can be submitted again later. The task is never marked as `processing`.

## Task lifecycle

```
pending ──Submit──► queued ──worker──► processing ──► completed
   ▲                  │                    │
   └── queue full ────┘                    └────────► failed
```

| From         | To           | When                                  |
|--------------|--------------|---------------------------------------|
| `pending`    | `queued`     | `Submit` accepted the task            |
| `queued`     | `pending`    | the queue was full, submit is undone  |
| `queued`     | `processing` | a worker took the task                |
| `processing` | `completed`  | analysis finished                     |
| `processing` | `failed`     | analysis returned an error            |

Any other change returns `ErrInvalidTransition`. `completed` and `failed` are final,
there are no retries. So submitting a task that is queued, processing, completed
or failed returns an error.

## Synchronization

**Store** (`task/store.go`)

- The map `map[int]Task` is protected by a `sync.RWMutex`. `Get` and `List` use the
  read lock, so many readers can work at the same time. All changes use the write lock.
- Tasks are stored as **values**, not pointers. `Get` and `List` return copies. Nobody
  outside the store can hold a pointer to a task and change its fields without the lock.
- Every status change is one method (`MarkQueued`, `MarkProcessing`, ...) that checks the
  current status and sets the new one **under the same lock**. Because of that, two
  goroutines can't both move a task from `pending` to `queued`, and a task can't be
  processed twice.
- The lock is never held while text is being analyzed. A worker takes the text under the
  lock, releases it, analyzes, then locks again only to save the result.

**Pool** (`worker/pool.go`)

- `sync.Mutex` protects the `stopped` flag and makes sending to the queue and closing
  the queue never happen at the same time. This is why a send on a closed channel
  can't happen.
- `Submit` never blocks while holding the mutex (the send is non-blocking), so it
  can't deadlock with `Stop`.
- `sync.WaitGroup` counts running workers.
- The pool owns the queue channel. Only `Stop` closes it, and only once.

## Stopping

`pool.Stop()`:

1. Under the mutex sets `stopped = true` and closes the queue (only the first time).
2. Releases the mutex and calls `wg.Wait()`.
3. Workers finish every task that is still in the queue (`range` over a closed channel
   reads the remaining values and then ends), call `wg.Done()` and exit.

After `Stop`, `Submit` returns `ErrPoolStopped` and the task stays `pending`.
Tasks that were accepted before `Stop` are always finished, nothing is dropped.
Calling `Stop` again is safe: it doesn't close the channel twice, it just waits.

## Running

Requires Go 1.27.

```bash
go run .
```

You can also pass your own texts:

```bash
go run . "Hello Go world" "Second text. With two sentences!"
```

## Tests

```bash
go test ./...
go test -race ./...
gofmt -l .
go vet ./...
```

What is tested:

- `analyzer`: validation and analysis results (table-driven tests), text without words.
- `task`: create, get, missing task, returned task is a copy, full lifecycle, failed
  task, invalid transitions (table-driven), independent tasks, concurrent create/get,
  only one of many concurrent `MarkQueued` calls succeeds.
- `worker`:
  - pool start and invalid configuration
  - `Submit` returns while the task is still being processed
  - 50 tasks each get their own correct result
  - two tasks are inside the processor **at the same time** on different workers
  - never more tasks at once than workers
  - duplicate / invalid / unknown task submissions
  - full queue: error, task stays `pending`, can be submitted again later
  - failed task
  - reading tasks while workers are running
  - the same task submitted from 20 goroutines is accepted and processed once
  - `Stop` waits for accepted tasks
  - `Submit` after `Stop`
  - `Stop` called many times
  - `Submit` and `Stop` at the same time: every task is either finished or still `pending`

Concurrency tests use a fake processor that waits on a channel, so they control
exactly when a task finishes. No `time.Sleep` is used to make them pass. `time.After`
is only a timeout so a broken test fails instead of hanging.
