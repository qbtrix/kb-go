// Pure text helpers shared by every layer: slugs, content hashes, word counts,
// truncation and nil-slice normalisation. Standard library only; nothing here
// touches the filesystem or the knowledge base.

package main

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
)

var (
	slugStripRe = regexp.MustCompile(`[^a-z0-9\s-]`)
	slugDashRe  = regexp.MustCompile(`[\s-]+`)
)

func slugify(s string) string {
	lower := strings.ToLower(s)
	clean := slugStripRe.ReplaceAllString(lower, "")
	slug := slugDashRe.ReplaceAllString(clean, "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 80 {
		slug = slug[:80]
	}
	if slug == "" {
		return contentHash(s)[:16]
	}
	return slug
}

func wordCount(s string) int {
	return len(strings.Fields(s))
}

func truncate(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func nilToEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func contentHash(text string) string {
	h := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%x", h)
}
