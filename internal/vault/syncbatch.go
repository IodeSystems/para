package vault

import (
	"sync"

	"github.com/iodesystems/parasolve/internal/atomicfile"
)

// syncEach fsyncs every file, then every directory. Files are synced by a few
// goroutines: concurrent fsyncs let the file system commit them together.
func syncEach(files, dirs []string) error {
	const workers = 16
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		first error
	)
	ch := make(chan string)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range ch {
				if err := syncFile(p); err != nil {
					mu.Lock()
					if first == nil {
						first = err
					}
					mu.Unlock()
				}
			}
		}()
	}
	for _, p := range files {
		ch <- p
	}
	close(ch)
	wg.Wait()
	if first != nil {
		return first
	}
	for _, d := range dirs {
		if err := atomicfile.SyncDir(d); err != nil {
			return err
		}
	}
	return nil
}
