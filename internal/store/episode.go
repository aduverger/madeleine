package store

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const maxL1Characters = 400

func validateEpisodeSummaries(l1, l2 string) (string, string, error) {
	l1 = strings.TrimSpace(l1)
	l2 = strings.TrimSpace(l2)
	if l1 == "" || l2 == "" {
		return "", "", fmt.Errorf("%w: Episode summaries must not be empty", ErrInvalidState)
	}
	if utf8.RuneCountInString(l1) > maxL1Characters {
		return "", "", fmt.Errorf("%w: Episode L1 exceeds %d Unicode characters", ErrInvalidState, maxL1Characters)
	}
	return l1, l2, nil
}
