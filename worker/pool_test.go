package worker

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"sis/analyzer"
	"sis/task"
)

// fakeProcessor blocks inside Process until the test lets it go.
// Sending to release lets one task finish, closing it lets all of them finish.
type fakeProcessor struct {
	started chan string
	release chan struct{}

	mu        sync.Mutex
	active    int
	maxActive int
}

func newFakeProcessor() *fakeProcessor {
	return &fakeProcessor{
		started: make(chan string, 100),
		release: make(chan struct{}),
	}
}

func (f *fakeProcessor) Process(text string) (analyzer.Result, error) {
	f.mu.Lock()
	f.active++
	if f.active > f.maxActive {
		f.maxActive = f.active
	}
	f.mu.Unlock()

	f.started <- text
	<-f.release

	f.mu.Lock()
	f.active--
	f.mu.Unlock()

	return analyzer.TextAnalyzer{}.Process(text)
}

func (f *fakeProcessor) max() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxActive
}

func waitStarted(t *testing.T, f *fakeProcessor, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-f.started:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of %d tasks started", i, n)
		}
	}
}

func stopWithTimeout(t *testing.T, p *Pool) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		p.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return, workers are stuck")
	}
}

func createTasks(t *testing.T, s *task.Store, texts ...string) []int {
	t.Helper()
	ids := make([]int, 0, len(texts))
	for _, text := range texts {
		created, err := s.Create(text)
		if err != nil {
			t.Fatalf("Create(%q): %v", text, err)
		}
		ids = append(ids, created.ID)
	}
	return ids
}

func mustSubmit(t *testing.T, p *Pool, id int) {
	t.Helper()
	if err := p.Submit(id); err != nil {
		t.Fatalf("Submit(%d): %v", id, err)
	}
}

func assertStatus(t *testing.T, s *task.Store, id int, want task.Status) task.Task {
	t.Helper()
	got, err := s.Get(id)
	if err != nil {
		t.Fatalf("Get(%d): %v", id, err)
	}
	if got.Status != want {
		t.Errorf("task %d status = %s, want %s", id, got.Status, want)
	}
	return got
}

func TestNewPool(t *testing.T) {
	tests := []struct {
		name      string
		workers   int
		queueSize int
		wantErr   error
	}{
		{"valid", 3, 10, nil},
		{"zero workers", 0, 10, ErrBadConfig},
		{"negative workers", -1, 10, ErrBadConfig},
		{"zero queue size", 3, 0, ErrBadConfig},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewPool(task.NewStore(), analyzer.TextAnalyzer{}, tt.workers, tt.queueSize)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got %v, want %v", err, tt.wantErr)
			}
			if err == nil {
				if p == nil {
					t.Fatal("pool is nil")
				}
				stopWithTimeout(t, p)
			}
		})
	}
}

func TestPoolProcessesTask(t *testing.T) {
	s := task.NewStore()
	p, _ := NewPool(s, analyzer.TextAnalyzer{}, 2, 5)
	ids := createTasks(t, s, "The cat sat. The cat ran!")

	mustSubmit(t, p, ids[0])
	stopWithTimeout(t, p)

	got := assertStatus(t, s, ids[0], task.StatusCompleted)
	want := analyzer.Result{Words: 6, UniqueWords: 4, Characters: 25, Sentences: 2, MostCommonWord: "cat"}
	if got.Result != want {
		t.Errorf("result = %+v, want %+v", got.Result, want)
	}
	if got.WorkerID < 1 || got.WorkerID > 2 {
		t.Errorf("worker id = %d, want 1 or 2", got.WorkerID)
	}
}

func TestSubmitDoesNotWaitForProcessing(t *testing.T) {
	s := task.NewStore()
	f := newFakeProcessor()
	p, _ := NewPool(s, f, 2, 5)
	ids := createTasks(t, s, "Hello world")

	mustSubmit(t, p, ids[0])

	waitStarted(t, f, 1)
	assertStatus(t, s, ids[0], task.StatusProcessing)

	close(f.release)
	stopWithTimeout(t, p)
	assertStatus(t, s, ids[0], task.StatusCompleted)
}

func TestManyTasksGetTheirOwnResults(t *testing.T) {
	s := task.NewStore()
	p, _ := NewPool(s, analyzer.TextAnalyzer{}, 4, 50)

	texts := make([]string, 50)
	for i := range texts {
		texts[i] = fmt.Sprintf("task%d %s", i, strings.Repeat("go ", i+1))
	}
	ids := createTasks(t, s, texts...)

	for _, id := range ids {
		mustSubmit(t, p, id)
	}
	stopWithTimeout(t, p)

	for i, id := range ids {
		got := assertStatus(t, s, id, task.StatusCompleted)
		if got.Result.Words != i+2 {
			t.Errorf("task %d words = %d, want %d", id, got.Result.Words, i+2)
		}
		if got.Result.MostCommonWord != "go" {
			t.Errorf("task %d most common word = %q, want %q", id, got.Result.MostCommonWord, "go")
		}
	}
}

func TestWorkersProcessTasksAtTheSameTime(t *testing.T) {
	s := task.NewStore()
	f := newFakeProcessor()
	p, _ := NewPool(s, f, 2, 5)
	ids := createTasks(t, s, "first task", "second task")

	mustSubmit(t, p, ids[0])
	mustSubmit(t, p, ids[1])

	// Both tasks are inside Process and neither has been released yet,
	// so they are running at the same time.
	waitStarted(t, f, 2)
	a := assertStatus(t, s, ids[0], task.StatusProcessing)
	b := assertStatus(t, s, ids[1], task.StatusProcessing)
	if a.WorkerID == b.WorkerID {
		t.Errorf("both tasks are on worker %d, want different workers", a.WorkerID)
	}

	close(f.release)
	stopWithTimeout(t, p)

	assertStatus(t, s, ids[0], task.StatusCompleted)
	assertStatus(t, s, ids[1], task.StatusCompleted)
	if f.max() != 2 {
		t.Errorf("max tasks at the same time = %d, want 2", f.max())
	}
}

func TestPoolNeverRunsMoreTasksThanWorkers(t *testing.T) {
	s := task.NewStore()
	f := newFakeProcessor()
	p, _ := NewPool(s, f, 3, 10)

	texts := make([]string, 10)
	for i := range texts {
		texts[i] = fmt.Sprintf("text number %d", i)
	}
	ids := createTasks(t, s, texts...)
	for _, id := range ids {
		mustSubmit(t, p, id)
	}

	waitStarted(t, f, 3)
	select {
	case <-f.started:
		t.Fatal("a fourth task started while all 3 workers were busy")
	default:
	}

	close(f.release)
	stopWithTimeout(t, p)

	for _, id := range ids {
		assertStatus(t, s, id, task.StatusCompleted)
	}
	if f.max() != 3 {
		t.Errorf("max tasks at the same time = %d, want 3", f.max())
	}
}

func TestSubmitInvalidTask(t *testing.T) {
	s := task.NewStore()
	f := newFakeProcessor()
	p, _ := NewPool(s, f, 1, 5)
	ids := createTasks(t, s, "processing task", "queued task", "completed task")
	processing, queued, completed := ids[0], ids[1], ids[2]

	mustSubmit(t, p, processing)
	waitStarted(t, f, 1)
	mustSubmit(t, p, queued)

	s.MarkQueued(completed)
	s.MarkProcessing(completed, 1)
	s.MarkCompleted(completed, analyzer.Result{})

	tests := []struct {
		name    string
		id      int
		wantErr error
	}{
		{"task that is processing", processing, task.ErrInvalidTransition},
		{"task that is already queued", queued, task.ErrInvalidTransition},
		{"completed task", completed, task.ErrInvalidTransition},
		{"task that does not exist", 999, task.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := p.Submit(tt.id); !errors.Is(err, tt.wantErr) {
				t.Errorf("got %v, want %v", err, tt.wantErr)
			}
		})
	}

	close(f.release)
	stopWithTimeout(t, p)

	assertStatus(t, s, processing, task.StatusCompleted)
	assertStatus(t, s, queued, task.StatusCompleted)
	if len(f.started) != 1 {
		t.Errorf("want only the queued task to start after release, got %d starts", len(f.started))
	}
}

func TestQueueFull(t *testing.T) {
	s := task.NewStore()
	f := newFakeProcessor()
	p, _ := NewPool(s, f, 1, 1)
	ids := createTasks(t, s, "first", "second", "third")

	mustSubmit(t, p, ids[0])
	waitStarted(t, f, 1)
	mustSubmit(t, p, ids[1])

	if err := p.Submit(ids[2]); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("got %v, want %v", err, ErrQueueFull)
	}
	assertStatus(t, s, ids[2], task.StatusPending)

	f.release <- struct{}{}
	waitStarted(t, f, 1)

	mustSubmit(t, p, ids[2])

	close(f.release)
	stopWithTimeout(t, p)
	for _, id := range ids {
		assertStatus(t, s, id, task.StatusCompleted)
	}
}

func TestFailedTask(t *testing.T) {
	s := task.NewStore()
	p, _ := NewPool(s, analyzer.TextAnalyzer{}, 2, 5)
	ids := createTasks(t, s, "!!! ???", "Hello world")

	mustSubmit(t, p, ids[0])
	mustSubmit(t, p, ids[1])
	stopWithTimeout(t, p)

	failed := assertStatus(t, s, ids[0], task.StatusFailed)
	if failed.Error != analyzer.ErrNoWords.Error() {
		t.Errorf("error = %q, want %q", failed.Error, analyzer.ErrNoWords.Error())
	}
	if failed.Result != (analyzer.Result{}) {
		t.Errorf("failed task must not have a result, got %+v", failed.Result)
	}
	assertStatus(t, s, ids[1], task.StatusCompleted)
}

func TestReadTasksWhileWorkersRun(t *testing.T) {
	s := task.NewStore()
	p, _ := NewPool(s, analyzer.TextAnalyzer{}, 4, 50)

	texts := make([]string, 50)
	for i := range texts {
		texts[i] = fmt.Sprintf("text number %d", i)
	}
	ids := createTasks(t, s, texts...)

	var wg sync.WaitGroup
	for r := 0; r < 5; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				got, err := s.Get(ids[i%len(ids)])
				if err != nil {
					t.Errorf("Get: %v", err)
					return
				}
				if got.Status == task.StatusFailed {
					t.Errorf("task %d failed: %s", got.ID, got.Error)
				}
				s.List()
			}
		}()
	}

	for _, id := range ids {
		mustSubmit(t, p, id)
	}
	wg.Wait()
	stopWithTimeout(t, p)

	for _, id := range ids {
		got := assertStatus(t, s, id, task.StatusCompleted)
		if got.Result.Words != 3 {
			t.Errorf("task %d words = %d, want 3", id, got.Result.Words)
		}
	}
}

func TestSameTaskSubmittedConcurrently(t *testing.T) {
	s := task.NewStore()
	f := newFakeProcessor()
	p, _ := NewPool(s, f, 2, 5)
	ids := createTasks(t, s, "Hello world")

	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if p.Submit(ids[0]) == nil {
				mu.Lock()
				accepted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	close(f.release)
	stopWithTimeout(t, p)

	if accepted != 1 {
		t.Errorf("task accepted %d times, want exactly 1", accepted)
	}
	if len(f.started) != 1 {
		t.Errorf("task processed %d times, want exactly 1", len(f.started))
	}
	assertStatus(t, s, ids[0], task.StatusCompleted)
}

func TestStopWaitsForAcceptedTasks(t *testing.T) {
	s := task.NewStore()
	f := newFakeProcessor()
	p, _ := NewPool(s, f, 1, 5)
	ids := createTasks(t, s, "first", "second", "third")
	for _, id := range ids {
		mustSubmit(t, p, id)
	}
	waitStarted(t, f, 1)

	done := make(chan struct{})
	go func() {
		p.Stop()
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Stop returned while a task was still being processed")
	default:
	}

	close(f.release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return")
	}

	for _, id := range ids {
		assertStatus(t, s, id, task.StatusCompleted)
	}
}

func TestSubmitAfterStop(t *testing.T) {
	s := task.NewStore()
	p, _ := NewPool(s, analyzer.TextAnalyzer{}, 2, 5)
	ids := createTasks(t, s, "Hello world")

	stopWithTimeout(t, p)

	if err := p.Submit(ids[0]); !errors.Is(err, ErrPoolStopped) {
		t.Fatalf("got %v, want %v", err, ErrPoolStopped)
	}
	assertStatus(t, s, ids[0], task.StatusPending)
}

func TestStopCalledManyTimes(t *testing.T) {
	p, _ := NewPool(task.NewStore(), analyzer.TextAnalyzer{}, 2, 5)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.Stop()
		}()
	}
	wg.Wait()

	stopWithTimeout(t, p)
}

func TestSubmitAndStopAtTheSameTime(t *testing.T) {
	s := task.NewStore()
	p, _ := NewPool(s, analyzer.TextAnalyzer{}, 4, 20)

	texts := make([]string, 200)
	for i := range texts {
		texts[i] = fmt.Sprintf("text number %d", i)
	}
	ids := createTasks(t, s, texts...)

	var wg sync.WaitGroup
	errs := make([]error, len(ids))
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = p.Submit(id)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		p.Stop()
	}()
	wg.Wait()
	stopWithTimeout(t, p)

	for i, id := range ids {
		switch {
		case errs[i] == nil:
			assertStatus(t, s, id, task.StatusCompleted)
		case errors.Is(errs[i], ErrPoolStopped), errors.Is(errs[i], ErrQueueFull):
			assertStatus(t, s, id, task.StatusPending)
		default:
			t.Errorf("task %d: unexpected error %v", id, errs[i])
		}
	}
}
