// Tests and benchmarks for the textutil helpers: Slugify (including the hash
// fallback and the 80-char cap), ContentHash, WordCount, Truncate, NilToEmpty.

package textutil

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/qbtrix/kb-go/internal/kbtest"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"Hello World", "hello-world"},
		{"GroupService — manages groups", "groupservice-manages-groups"},
		{"foo/bar/baz.py", "foobarbazpy"},
		{"", ""}, // falls back to hash
		{"UPPER CASE", "upper-case"},
		{"special!@#chars", "specialchars"},
	}
	for _, tt := range tests {
		got := Slugify(tt.input)
		if tt.input == "" {
			if len(got) != 16 { // hash fallback
				t.Errorf("slugify(%q) = %q, want 16-char hash", tt.input, got)
			}
			continue
		}
		if got != tt.want {
			t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSlugifyLongTitle(t *testing.T) {
	long := strings.Repeat("a", 200)
	got := Slugify(long)
	if len(got) > 80 {
		t.Errorf("slugify should truncate to 80 chars, got %d", len(got))
	}
}

func TestContentHash(t *testing.T) {
	h1 := ContentHash("hello world")
	h2 := ContentHash("hello world")
	h3 := ContentHash("hello world!")

	if h1 != h2 {
		t.Error("same input should produce same hash")
	}
	if h1 == h3 {
		t.Error("different input should produce different hash")
	}
	if len(h1) != 64 { // SHA256 hex
		t.Errorf("hash length should be 64, got %d", len(h1))
	}
}

func TestWordCount(t *testing.T) {
	if WordCount("hello world") != 2 {
		t.Error("wordCount('hello world') != 2")
	}
	if WordCount("") != 0 {
		t.Error("wordCount('') != 0")
	}
	if WordCount("  spaced  out  ") != 2 {
		t.Error("wordCount with extra spaces")
	}
}

func TestTruncate(t *testing.T) {
	if Truncate("hello", 10) != "hello" {
		t.Error("short string should not be truncated")
	}
	result := Truncate("hello world foo bar", 10)
	if len(result) > 13 { // 10 + "..."
		t.Errorf("truncated string too long: %q", result)
	}
	if !strings.HasSuffix(result, "...") {
		t.Errorf("should end with ...: %q", result)
	}
}

func TestNilToEmpty(t *testing.T) {
	var nilSlice []string
	result := NilToEmpty(nilSlice)
	if result == nil {
		t.Error("nilToEmpty should return empty slice, not nil")
	}
	if len(result) != 0 {
		t.Error("nilToEmpty should return empty slice")
	}

	existing := []string{"a", "b"}
	result2 := NilToEmpty(existing)
	if len(result2) != 2 {
		t.Error("nilToEmpty should preserve existing slice")
	}
}

func BenchmarkContentHash(b *testing.B) {
	sizes := map[string]int{"1KB": 1024, "10KB": 10240, "100KB": 102400}
	for name, size := range sizes {
		data := strings.Repeat("x", size)
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				ContentHash(data)
			}
		})
	}
}

func BenchmarkSlugify(b *testing.B) {
	inputs := []string{
		"Simple Title",
		"GroupService — manages group operations and membership",
		"A Very Long Title That Should Be Truncated Because It Exceeds The Maximum Length Allowed",
		"special!@#$%^&*()chars",
	}
	for _, input := range inputs {
		b.Run(fmt.Sprintf("len_%d", len(input)), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				Slugify(input)
			}
		})
	}
}

func TestMain(m *testing.M) {
	os.Exit(kbtest.Main(m))
}
