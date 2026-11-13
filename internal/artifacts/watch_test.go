package artifacts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Watch reports artifacts of run directories, new ones included, and leaves
// hidden folders (archify's working files) alone.
func TestWatchSkipsHiddenFolders(t *testing.T) {
	root := t.TempDir()
	run := filepath.Join(root, "core", "1")
	os.MkdirAll(filepath.Join(run, ".archify", "old"), 0o755)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var got []string
	if err := Watch(ctx, root, func(p string) { mu.Lock(); got = append(got, p); mu.Unlock() }); err != nil {
		t.Fatal(err)
	}
	os.Rename(filepath.Join(run, ".archify", "old"), filepath.Join(run, ".archify", "new"))
	time.Sleep(100 * time.Millisecond) // time to watch the renamed folder, were it watched
	os.WriteFile(filepath.Join(run, ".archify", "new", "x.md"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(root, "core", "2"), 0o755)
	time.Sleep(100 * time.Millisecond) // the new run directory gets its watch
	os.WriteFile(filepath.Join(run, "PLAN-1.md"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(root, "core", "2", "PLAN-1.md"), []byte("x"), 0o644)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("reported %v, want the two PLAN-1.md", got)
	}
	for _, p := range got {
		if strings.Contains(p, ".archify") {
			t.Fatalf("reported a hidden folder's file: %v", got)
		}
	}
}
