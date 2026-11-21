package worker

import (
	"errors"
	"sync"

	"sis/analyzer"
	"sis/task"
)

var (
	ErrQueueFull   = errors.New("queue is full")
	ErrPoolStopped = errors.New("pool is stopped")
	ErrBadConfig   = errors.New("workers and queue size must be greater than zero")
)

type Processor interface {
	Process(text string) (analyzer.Result, error)
}

type Pool struct {
	store     *task.Store
	processor Processor
	queue     chan int
	wg        sync.WaitGroup

	mu      sync.Mutex
	stopped bool
}

func NewPool(store *task.Store, processor Processor, workers, queueSize int) (*Pool, error) {
	if workers <= 0 || queueSize <= 0 {
		return nil, ErrBadConfig
	}

	p := &Pool{
		store:     store,
		processor: processor,
		queue:     make(chan int, queueSize),
	}

	for i := 1; i <= workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
	return p, nil
}

// Submit never waits: if the queue is full it returns ErrQueueFull
// and the task goes back to pending.
func (p *Pool) Submit(id int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stopped {
		return ErrPoolStopped
	}

	if err := p.store.MarkQueued(id); err != nil {
		return err
	}

	select {
	case p.queue <- id:
		return nil
	default:
		p.store.MarkPending(id)
		return ErrQueueFull
	}
}

// Stop rejects new tasks, lets workers finish everything already
// in the queue and waits for them to exit. Calling it again is safe.
func (p *Pool) Stop() {
	p.mu.Lock()
	if !p.stopped {
		p.stopped = true
		close(p.queue)
	}
	p.mu.Unlock()

	p.wg.Wait()
}

func (p *Pool) worker(id int) {
	defer p.wg.Done()

	for taskID := range p.queue {
		p.process(id, taskID)
	}
}

func (p *Pool) process(workerID, taskID int) {
	text, err := p.store.MarkProcessing(taskID, workerID)
	if err != nil {
		return
	}

	result, err := p.processor.Process(text)
	if err != nil {
		p.store.MarkFailed(taskID, err)
		return
	}
	p.store.MarkCompleted(taskID, result)
}
