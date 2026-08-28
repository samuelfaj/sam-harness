package traceability

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestEveryBookChapterHasAnImplementationMapping(t *testing.T) {
	t.Parallel()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "docs", "book-traceability.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	pathPattern := regexp.MustCompile("`([^`]+)`")
	for chapter := 1; chapter <= 20; chapter++ {
		prefix := "| " + strconv.Itoa(chapter) + " |"
		if strings.Count(text, prefix) != 1 {
			t.Fatalf("chapter %d must appear exactly once", chapter)
		}
		var row string
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(line, prefix) {
				row = line
				break
			}
		}
		match := pathPattern.FindStringSubmatch(row)
		if len(match) != 2 {
			t.Fatalf("chapter %d has no implementation path", chapter)
		}
		implementation := filepath.Join(filepath.Dir(file), "..", "..", filepath.FromSlash(match[1]))
		if _, err := os.Stat(implementation); err != nil {
			t.Fatalf("chapter %d points to missing implementation %s", chapter, match[1])
		}
	}
}
