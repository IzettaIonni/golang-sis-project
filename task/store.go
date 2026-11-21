package task

import (
	"fmt"
	"sort"
	"sync"

	"sis/analyzer"
)

// Tasks are stored as values, not pointers. Get and List return copies,
// so code outside the store can never change a task without the lock.
type Store struct {
	mu     sync.RWMutex
	tasks  map[int]Task
	nextID int
}

func NewStore() *Store {
	return &Store{
		tasks:  make(map[int]Task),
		nextID: 1,
	}
}

func (s *Store) Create(text string) (Task, error) {
	if err := analyzer.Validate(text); err != nil {
		return Task{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	t := Task{ID: s.nextID, Text: text, Status: StatusPending}
	s.tasks[t.ID] = t
	s.nextID++
	return t, nil
}

func (s *Store) Get(id int) (Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	t, ok := s.tasks[id]
	if !ok {
		return Task{}, fmt.Errorf("%w: id %d", ErrNotFound, id)
	}
	return t, nil
}

func (s *Store) List() []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].ID < list[j].ID
	})
	return list
}

func (s *Store) MarkQueued(id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, err := s.find(id, StatusPending)
	if err != nil {
		return err
	}
	t.Status = StatusQueued
	s.tasks[id] = t
	return nil
}

func (s *Store) MarkPending(id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, err := s.find(id, StatusQueued)
	if err != nil {
		return err
	}
	t.Status = StatusPending
	s.tasks[id] = t
	return nil
}

func (s *Store) MarkProcessing(id, workerID int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, err := s.find(id, StatusQueued)
	if err != nil {
		return "", err
	}
	t.Status = StatusProcessing
	t.WorkerID = workerID
	s.tasks[id] = t
	return t.Text, nil
}

func (s *Store) MarkCompleted(id int, result analyzer.Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, err := s.find(id, StatusProcessing)
	if err != nil {
		return err
	}
	t.Status = StatusCompleted
	t.Result = result
	s.tasks[id] = t
	return nil
}

func (s *Store) MarkFailed(id int, reason error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, err := s.find(id, StatusProcessing)
	if err != nil {
		return err
	}
	t.Status = StatusFailed
	t.Error = reason.Error()
	s.tasks[id] = t
	return nil
}

// find must be called with s.mu already locked.
func (s *Store) find(id int, want Status) (Task, error) {
	t, ok := s.tasks[id]
	if !ok {
		return Task{}, fmt.Errorf("%w: id %d", ErrNotFound, id)
	}
	if t.Status != want {
		return Task{}, fmt.Errorf("%w: task %d is %s, expected %s", ErrInvalidTransition, id, t.Status, want)
	}
	return t, nil
}
