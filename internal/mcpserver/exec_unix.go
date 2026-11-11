//go:build linux || darwin

package mcpserver

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/iodesystems/para/internal/opstats"
)

// fdStdin reads fd 0: wait polls it without taking a byte, read takes what
// waits. A byte is in this process only once read returned, under the
// gate's lock, so an exec under that lock loses none.
type fdStdin struct{ fd int }

// fdSource is the fd source of f when f is stdin, else nil.
func fdSource(f *os.File) source {
	if f == nil || f.Fd() != 0 {
		return nil
	}
	return &fdStdin{fd: 0}
}

func (s *fdStdin) wait() error {
	fds := []unix.PollFd{{Fd: int32(s.fd), Events: unix.POLLIN}}
	for {
		_, err := unix.Poll(fds, -1)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		return err
	}
}

func (s *fdStdin) read(p []byte) (int, error) {
	for {
		n, err := syscall.Read(s.fd, p)
		switch {
		case errors.Is(err, syscall.EINTR):
			continue
		case errors.Is(err, syscall.EAGAIN):
			// A non-blocking stdin that poll woke for nothing.
			return 0, nil
		case err != nil:
			return 0, err
		case n == 0:
			return 0, io.EOF
		}
		return n, nil
	}
}

func (*fdStdin) safe() bool { return true }

// execBinary writes h to a file in dir and execs exe with this process's
// argv and environment, plus HandoffEnv naming the file. stdin, stdout, and
// stderr carry over; every other file para opens is close-on-exec. It
// returns only on failure, and then removes the file.
func execBinary(dir, exe string, h Handoff) error {
	path, err := writeHandoff(dir, h)
	if err != nil {
		return err
	}
	_ = opstats.FlushAll()
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, HandoffEnv+"=") {
			env = append(env, kv)
		}
	}
	env = append(env, HandoffEnv+"="+path)
	err = syscall.Exec(exe, os.Args, env)
	_ = os.Remove(path)
	return fmt.Errorf("mcpserver: exec %s: %w", exe, err)
}
