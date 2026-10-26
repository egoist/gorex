//go:build darwin || linux

package rex

import (
	"bytes"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// TestMain runs the test binary as a server when Spawn or an upgrade
// starts it so, as main does the app's.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "-server" {
		log.SetPrefix("[server] ")
		serve := Serve
		if len(os.Args) > 3 && os.Args[2] == "-resume" {
			serve = func() error { return Resume(os.Args[3]) }
		}
		if err := serve(); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// readUntil reads s until what it read matches re, for a few seconds.
func readUntil(t *testing.T, s *Stream, re string) string {
	t.Helper()
	var out bytes.Buffer
	buf := make([]byte, 4096)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !regexp.MustCompile(re).Match(out.Bytes()) {
			n, err := s.Read(buf)
			out.Write(buf[:n])
			if err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		s.Close()
		<-done
	}
	if !regexp.MustCompile(re).Match(out.Bytes()) {
		t.Fatalf("no %q in %q", re, out.String())
	}
	return out.String()
}

// TestUpgrade upgrades a server, of a process of its own, to another
// executable: the session, its shell and what it printed stay.
func TestUpgrade(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "rex")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("GOREX_DIR", dir)
	// The server is this test binary, started by Spawn.
	c, err := Connect()
	if err != nil {
		t.Fatal(err)
	}
	h, err := c.Hello()
	if err != nil || !h.Upgrade {
		t.Fatalf("hello %+v %v", h, err)
	}
	defer func() {
		c.Shutdown()
		c.Close()
		time.Sleep(100 * time.Millisecond)
		if processAlive(h.PID) {
			syscall.Kill(h.PID, syscall.SIGKILL)
		}
		if b, err := os.ReadFile(filepath.Join(dir, "server.log")); err == nil && t.Failed() {
			t.Logf("server.log:\n%s", b)
		}
	}()
	info, err := c.Create(CreateOptions{Command: []string{"/bin/sh"}, Dir: "/tmp", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	s := c.Stream(info.ID, 80, 24)
	s.Resize(80, 24)
	s.Write([]byte("X=42; echo before-''upgrade; sleep 1000 & echo sleeper=$!\n"))
	out := readUntil(t, s, `sleeper=(\d+)`)
	sleeper, _ := strconv.Atoi(regexp.MustCompile(`sleeper=(\d+)`).FindStringSubmatch(out)[1])
	defer syscall.Kill(sleeper, syscall.SIGKILL)
	s.Close()

	// An executable that does not run leaves the server as it was.
	bad := filepath.Join(dir, "not-executable")
	os.WriteFile(bad, []byte("nothing"), 0o600)
	if _, err := c.Upgrade(bad); err == nil {
		t.Fatal("upgraded to a file that does not run")
	}
	s = c.Stream(info.ID, 80, 24)
	s.Resize(80, 24)
	s.Write([]byte("echo still-''here\n"))
	readUntil(t, s, `still-here`)
	s.Close()

	// A copy of the test binary is another executable.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "rex-upgraded.test")
	if err := copyFile(self, exe); err != nil {
		t.Fatal(err)
	}
	c2, err := c.Upgrade(exe)
	if err != nil {
		t.Fatal(err)
	}
	c = c2
	h2, err := c.Hello()
	wantExe, wantTime := executable(exe)
	if err != nil || h2.PID != h.PID || h2.Exe != wantExe || !h2.ExeTime.Equal(wantTime) || h2.Exe == h.Exe {
		t.Fatalf("hello after upgrade %+v %v, before %+v", h2, err, h)
	}
	list, err := c.List()
	if err != nil || len(list) != 1 || list[0].ID != info.ID || list[0].PID != info.PID || list[0].Exited {
		t.Fatalf("list %+v %v, created %+v", list, err, info)
	}
	if !processAlive(info.PID) || !processAlive(sleeper) {
		t.Fatal("the shell or its job is gone")
	}
	s = c.Stream(info.ID, 80, 24)
	s.Resize(80, 24)
	readUntil(t, s, `before-upgrade`)
	s.Write([]byte("echo x$X\n"))
	readUntil(t, s, `x42`)
	s.Write([]byte("exit 3\n"))
	var in SessionInfo
	for deadline := time.Now().Add(5 * time.Second); !in.Exited && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if list, err := c.List(); err == nil && len(list) == 1 {
			in = list[0]
		}
	}
	if !in.Exited || in.ExitCode != 3 {
		t.Fatalf("after exit %+v", in)
	}
	s.Close()
}

func copyFile(from, to string) error {
	r, err := os.Open(from)
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, r); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}
