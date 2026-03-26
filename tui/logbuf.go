package tui

import (
	"os"
	"strings"
	"sync"
)

const logBufMaxLines = 16384

type LogBuffer struct {
	mu    sync.Mutex
	lines [logBufMaxLines]string
	pos   int
	total int
}

func NewLogBuffer() *LogBuffer { return &LogBuffer{} }

func (b *LogBuffer) Write(p []byte) (int, error) {
	s := strings.TrimRight(string(p), "\n")
	b.mu.Lock()
	b.lines[b.pos] = s
	b.pos = (b.pos + 1) % logBufMaxLines
	b.total++
	b.mu.Unlock()
	return len(p), nil
}

func (b *LogBuffer) Lines() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.total == 0 {
		return nil
	}
	n := b.total
	if n > logBufMaxLines {
		n = logBufMaxLines
	}
	result := make([]string, n)
	start := (b.pos - n + logBufMaxLines) % logBufMaxLines
	for i := 0; i < n; i++ {
		result[i] = b.lines[(start+i)%logBufMaxLines]
	}
	return result
}

func (b *LogBuffer) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.total
}

func (b *LogBuffer) SaveToFile(path string) error {
	lines := b.Lines()
	var sb strings.Builder
	for _, l := range lines {
		sb.WriteString(l)
		sb.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(sb.String()), 0644)
}
