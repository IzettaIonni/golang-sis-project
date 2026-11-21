package analyzer

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	ErrEmptyText = errors.New("text is empty")
	ErrNoWords   = errors.New("text contains no words")
)

type Result struct {
	Words          int
	UniqueWords    int
	Characters     int
	Sentences      int
	MostCommonWord string
}

type TextAnalyzer struct{}

func Validate(text string) error {
	if strings.TrimSpace(text) == "" {
		return ErrEmptyText
	}
	return nil
}

func (TextAnalyzer) Process(text string) (Result, error) {
	words := splitWords(text)
	if len(words) == 0 {
		return Result{}, ErrNoWords
	}

	counts := make(map[string]int)
	for _, w := range words {
		counts[w]++
	}

	return Result{
		Words:          len(words),
		UniqueWords:    len(counts),
		Characters:     utf8.RuneCountInString(text),
		Sentences:      countSentences(text),
		MostCommonWord: mostCommon(counts),
	}, nil
}

func splitWords(text string) []string {
	parts := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\''
	})

	words := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(p, "'")
		if p != "" {
			words = append(words, p)
		}
	}
	return words
}

func countSentences(text string) int {
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return r == '.' || r == '!' || r == '?'
	})

	count := 0
	for _, p := range parts {
		if len(splitWords(p)) > 0 {
			count++
		}
	}
	return count
}

// On a tie the alphabetically first word wins, so the result is always the same.
func mostCommon(counts map[string]int) string {
	best, bestCount := "", 0
	for w, c := range counts {
		if c > bestCount || (c == bestCount && w < best) {
			best, bestCount = w, c
		}
	}
	return best
}
