package github

import (
	"fmt"
	"strings"
	"unicode"
)

// splitQuery separates GitHub search terms without removing quoted text.
func splitQuery(query string) ([]string, error) {
	var terms []string
	var current strings.Builder
	var quote rune
	escaped := false

	flush := func() {
		if current.Len() == 0 {
			return
		}
		terms = append(terms, current.String())
		current.Reset()
	}

	for _, char := range query {
		if escaped {
			current.WriteRune(char)
			escaped = false
			continue
		}
		if char == '\\' {
			current.WriteRune(char)
			escaped = true
			continue
		}
		if quote != 0 {
			current.WriteRune(char)
			if char == quote {
				quote = 0
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			current.WriteRune(char)
			continue
		}
		if unicode.IsSpace(char) {
			flush()
			continue
		}
		current.WriteRune(char)
	}
	if quote != 0 {
		return nil, fmt.Errorf("unclosed %c quote in GitHub query", quote)
	}
	flush()
	if len(terms) == 0 {
		return nil, fmt.Errorf("GitHub query is empty")
	}
	return terms, nil
}
