package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLogBuffer_Write(t *testing.T) {
	b := NewLogBuffer()
	b.Write([]byte("lorem ipsum\n"))
	b.Write([]byte("dolor sit amet\n"))

	lines := b.Lines()
	if len(lines) != 2 {
		t.Fatalf("len = %d, want 2", len(lines))
	}
	if lines[0] != "lorem ipsum" {
		t.Errorf("[0] = %q", lines[0])
	}
	if lines[1] != "dolor sit amet" {
		t.Errorf("[1] = %q", lines[1])
	}
}

func TestLogBuffer_Empty(t *testing.T) {
	b := NewLogBuffer()
	lines := b.Lines()
	if len(lines) != 0 {
		t.Errorf("len = %d, want 0", len(lines))
	}
}

func TestLogBuffer_RingOverflow(t *testing.T) {
	b := NewLogBuffer()
	for i := 0; i < logBufMaxLines+100; i++ {
		fmt.Fprintf(b, "line %d\n", i)
	}
	lines := b.Lines()
	if len(lines) != logBufMaxLines {
		t.Fatalf("len = %d, want %d", len(lines), logBufMaxLines)
	}
	if lines[0] != "line 100" {
		t.Errorf("first = %q, want 'line 100'", lines[0])
	}
	last := fmt.Sprintf("line %d", logBufMaxLines+99)
	if lines[len(lines)-1] != last {
		t.Errorf("last = %q, want %q", lines[len(lines)-1], last)
	}
}

func TestLogBuffer_Count(t *testing.T) {
	b := NewLogBuffer()
	b.Write([]byte("a\n"))
	b.Write([]byte("b\n"))
	b.Write([]byte("c\n"))
	if b.Count() != 3 {
		t.Errorf("count = %d, want 3", b.Count())
	}
}

func TestLogBuffer_Concurrent(t *testing.T) {
	b := NewLogBuffer()
	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				fmt.Fprintf(b, "g%d line %d\n", id, i)
			}
		}(g)
	}
	wg.Wait()
	lines := b.Lines()
	if len(lines) != 1000 {
		t.Errorf("len = %d, want 1000", len(lines))
	}
}

func TestLogBuffer_SaveToFile(t *testing.T) {
	b := NewLogBuffer()
	b.Write([]byte("lorem\n"))
	b.Write([]byte("ipsum\n"))
	b.Write([]byte("dolor\n"))

	path := filepath.Join(t.TempDir(), "test.log")
	if err := b.SaveToFile(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "lorem\n") {
		t.Error("missing lorem")
	}
	if !strings.Contains(content, "dolor\n") {
		t.Error("missing dolor")
	}
}
