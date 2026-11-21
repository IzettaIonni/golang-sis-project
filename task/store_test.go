package task

import (
	"errors"
	"sync"
	"testing"

	"sis/analyzer"
)

func TestCreate(t *testing.T) {
	s := NewStore()

	first, err := s.Create("Hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := s.Create("Another text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if first.ID == second.ID {
		t.Errorf("ids must be unique, both are %d", first.ID)
	}
	if first.Status != StatusPending {
		t.Errorf("status = %s, want %s", first.Status, StatusPending)
	}
	if first.Text != "Hello world" {
		t.Errorf("text = %q, want %q", first.Text, "Hello world")
	}
}

func TestCreateInvalid(t *testing.T) {
	s := NewStore()

	_, err := s.Create("   ")
	if !errors.Is(err, analyzer.ErrEmptyText) {
		t.Fatalf("got %v, want %v", err, analyzer.ErrEmptyText)
	}
	if len(s.List()) != 0 {
		t.Errorf("invalid task must not be stored")
	}
}

func TestGet(t *testing.T) {
	s := NewStore()
	created, _ := s.Create("Hello world")

	got, err := s.Get(created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != created {
		t.Errorf("got %+v, want %+v", got, created)
	}
}

func TestGetNotFound(t *testing.T) {
	s := NewStore()

	_, err := s.Get(42)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want %v", err, ErrNotFound)
	}
}

func TestGetReturnsCopy(t *testing.T) {
	s := NewStore()
	created, _ := s.Create("Hello world")

	got, _ := s.Get(created.ID)
	got.Status = StatusCompleted
	got.Text = "changed"

	again, _ := s.Get(created.ID)
	if again.Status != StatusPending || again.Text != "Hello world" {
		t.Errorf("changing a returned task must not change the store, got %+v", again)
	}
}

func TestFullLifecycle(t *testing.T) {
	s := NewStore()
	created, _ := s.Create("Hello world")
	result := analyzer.Result{Words: 2, UniqueWords: 2, Characters: 11, Sentences: 1, MostCommonWord: "hello"}

	if err := s.MarkQueued(created.ID); err != nil {
		t.Fatalf("MarkQueued: %v", err)
	}
	text, err := s.MarkProcessing(created.ID, 1)
	if err != nil {
		t.Fatalf("MarkProcessing: %v", err)
	}
	if text != "Hello world" {
		t.Errorf("MarkProcessing returned %q, want task text", text)
	}
	if err := s.MarkCompleted(created.ID, result); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}

	got, _ := s.Get(created.ID)
	if got.Status != StatusCompleted {
		t.Errorf("status = %s, want %s", got.Status, StatusCompleted)
	}
	if got.Result != result {
		t.Errorf("result = %+v, want %+v", got.Result, result)
	}
	if got.WorkerID != 1 {
		t.Errorf("worker id = %d, want 1", got.WorkerID)
	}
}

func TestMarkFailed(t *testing.T) {
	s := NewStore()
	created, _ := s.Create("Hello world")
	s.MarkQueued(created.ID)
	s.MarkProcessing(created.ID, 1)

	if err := s.MarkFailed(created.ID, analyzer.ErrNoWords); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	got, _ := s.Get(created.ID)
	if got.Status != StatusFailed {
		t.Errorf("status = %s, want %s", got.Status, StatusFailed)
	}
	if got.Error != analyzer.ErrNoWords.Error() {
		t.Errorf("error = %q, want %q", got.Error, analyzer.ErrNoWords.Error())
	}
}

func TestInvalidTransitions(t *testing.T) {
	tests := []struct {
		name   string
		status Status
		action func(s *Store, id int) error
	}{
		{
			name:   "process a pending task",
			status: StatusPending,
			action: func(s *Store, id int) error { _, err := s.MarkProcessing(id, 1); return err },
		},
		{
			name:   "complete a queued task",
			status: StatusQueued,
			action: func(s *Store, id int) error { return s.MarkCompleted(id, analyzer.Result{}) },
		},
		{
			name:   "queue a processing task",
			status: StatusProcessing,
			action: func(s *Store, id int) error { return s.MarkQueued(id) },
		},
		{
			name:   "queue a completed task",
			status: StatusCompleted,
			action: func(s *Store, id int) error { return s.MarkQueued(id) },
		},
		{
			name:   "queue a failed task",
			status: StatusFailed,
			action: func(s *Store, id int) error { return s.MarkQueued(id) },
		},
		{
			name:   "fail a completed task",
			status: StatusCompleted,
			action: func(s *Store, id int) error { return s.MarkFailed(id, errors.New("boom")) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStore()
			id := createWithStatus(t, s, tt.status)

			err := tt.action(s, id)
			if !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("got %v, want %v", err, ErrInvalidTransition)
			}

			got, _ := s.Get(id)
			if got.Status != tt.status {
				t.Errorf("status changed to %s after a rejected operation", got.Status)
			}
		})
	}
}

func TestTransitionOnMissingTask(t *testing.T) {
	s := NewStore()

	if err := s.MarkQueued(7); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want %v", err, ErrNotFound)
	}
}

func TestMultipleTasksAreIndependent(t *testing.T) {
	s := NewStore()
	a, _ := s.Create("first text")
	b, _ := s.Create("second text")
	resultA := analyzer.Result{Words: 2, MostCommonWord: "first"}

	s.MarkQueued(a.ID)
	s.MarkProcessing(a.ID, 1)
	s.MarkCompleted(a.ID, resultA)

	gotA, _ := s.Get(a.ID)
	gotB, _ := s.Get(b.ID)

	if gotA.Result != resultA {
		t.Errorf("task a result = %+v, want %+v", gotA.Result, resultA)
	}
	if gotB.Status != StatusPending || gotB.Result != (analyzer.Result{}) {
		t.Errorf("task b must stay untouched, got %+v", gotB)
	}
}

func TestConcurrentCreateAndGet(t *testing.T) {
	s := NewStore()
	const n = 100

	var wg sync.WaitGroup
	ids := make(chan int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			created, err := s.Create("some text")
			if err != nil {
				t.Errorf("Create: %v", err)
				return
			}
			if _, err := s.Get(created.ID); err != nil {
				t.Errorf("Get: %v", err)
			}
			s.List()
			ids <- created.ID
		}()
	}
	wg.Wait()
	close(ids)

	seen := make(map[int]bool)
	for id := range ids {
		if seen[id] {
			t.Fatalf("duplicate id %d", id)
		}
		seen[id] = true
	}
	if len(seen) != n || len(s.List()) != n {
		t.Errorf("want %d tasks, got %d ids and %d stored", n, len(seen), len(s.List()))
	}
}

func TestConcurrentMarkQueuedOnlyOneWins(t *testing.T) {
	s := NewStore()
	created, _ := s.Create("Hello world")

	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.MarkQueued(created.ID) == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if successes != 1 {
		t.Errorf("MarkQueued succeeded %d times, want exactly 1", successes)
	}
}

func createWithStatus(t *testing.T, s *Store, status Status) int {
	t.Helper()
	created, err := s.Create("Hello world")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := created.ID

	if status == StatusPending {
		return id
	}
	s.MarkQueued(id)
	if status == StatusQueued {
		return id
	}
	s.MarkProcessing(id, 1)
	switch status {
	case StatusCompleted:
		s.MarkCompleted(id, analyzer.Result{})
	case StatusFailed:
		s.MarkFailed(id, errors.New("boom"))
	}
	return id
}
