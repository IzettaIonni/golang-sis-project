package analyzer

import (
	"errors"
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantErr error
	}{
		{"normal text", "Hello Go world", nil},
		{"single word", "Go", nil},
		{"empty", "", ErrEmptyText},
		{"only spaces", "     ", ErrEmptyText},
		{"tabs and newlines", "\t\n  \n", ErrEmptyText},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.text)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate(%q) = %v, want %v", tt.text, err, tt.wantErr)
			}
		})
	}
}

func TestProcess(t *testing.T) {
	tests := []struct {
		name string
		text string
		want Result
	}{
		{
			name: "simple sentence",
			text: "Hello Go world",
			want: Result{Words: 3, UniqueWords: 3, Characters: 14, Sentences: 1, MostCommonWord: "go"},
		},
		{
			name: "repeated words",
			text: "The cat sat. The cat ran!",
			want: Result{Words: 6, UniqueWords: 4, Characters: 25, Sentences: 2, MostCommonWord: "cat"},
		},
		{
			name: "cyrillic counts runes not bytes",
			text: "Привет, мир! Привет?",
			want: Result{Words: 3, UniqueWords: 2, Characters: 20, Sentences: 2, MostCommonWord: "привет"},
		},
		{
			name: "apostrophes",
			text: "Don't stop. don't 'quote'",
			want: Result{Words: 4, UniqueWords: 3, Characters: 25, Sentences: 2, MostCommonWord: "don't"},
		},
		{
			name: "several punctuation marks in a row",
			text: "Wait... what?!",
			want: Result{Words: 2, UniqueWords: 2, Characters: 14, Sentences: 2, MostCommonWord: "wait"},
		},
	}

	var a TextAnalyzer
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := a.Process(tt.text)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Process(%q)\n got  %+v\n want %+v", tt.text, got, tt.want)
			}
		})
	}
}

func TestProcessNoWords(t *testing.T) {
	var a TextAnalyzer
	_, err := a.Process("!!! ???")
	if !errors.Is(err, ErrNoWords) {
		t.Errorf("got %v, want %v", err, ErrNoWords)
	}
}
