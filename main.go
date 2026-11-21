package main

import (
	"fmt"
	"os"

	"sis/analyzer"
	"sis/task"
	"sis/worker"
)

const (
	workers   = 3
	queueSize = 10
)

func main() {
	texts := []string{
		"Go is an open source programming language. Go makes it easy to build simple, reliable software.",
		"The quick brown fox jumps over the lazy dog!",
		"Привет, мир! Это проверка текста на русском языке.",
		"Concurrency is not parallelism. Concurrency is about dealing with lots of things at once.",
		"   ",
		"?!...",
		"Don't communicate by sharing memory; share memory by communicating.",
	}
	if len(os.Args) > 1 {
		texts = os.Args[1:]
	}

	store := task.NewStore()
	pool, err := worker.NewPool(store, analyzer.TextAnalyzer{}, workers, queueSize)
	if err != nil {
		fmt.Println("cannot start pool:", err)
		os.Exit(1)
	}
	fmt.Printf("started %d workers, queue size %d\n\n", workers, queueSize)

	for _, text := range texts {
		t, err := store.Create(text)
		if err != nil {
			fmt.Printf("not created %q: %v\n", text, err)
			continue
		}
		if err := pool.Submit(t.ID); err != nil {
			fmt.Printf("task %d not submitted: %v\n", t.ID, err)
			continue
		}
		fmt.Printf("task %d submitted\n", t.ID)
	}

	if err := pool.Submit(1); err != nil {
		fmt.Printf("submit task 1 again: %v\n", err)
	}

	pool.Stop()
	fmt.Println("\npool stopped, all accepted tasks are finished")

	late, _ := store.Create("This task comes after stop.")
	if err := pool.Submit(late.ID); err != nil {
		fmt.Printf("submit task %d after stop: %v\n", late.ID, err)
	}

	for _, t := range store.List() {
		printTask(t)
	}
}

func printTask(t task.Task) {
	fmt.Printf("\ntask %d [%s]", t.ID, t.Status)
	if t.WorkerID != 0 {
		fmt.Printf(" by worker %d", t.WorkerID)
	}
	fmt.Printf("\n  text: %q\n", t.Text)

	switch t.Status {
	case task.StatusCompleted:
		r := t.Result
		fmt.Printf("  words: %d, unique: %d, characters: %d, sentences: %d, most common: %q\n",
			r.Words, r.UniqueWords, r.Characters, r.Sentences, r.MostCommonWord)
	case task.StatusFailed:
		fmt.Printf("  error: %s\n", t.Error)
	}
}
