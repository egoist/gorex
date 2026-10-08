package rex

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"testing"
	"time"
)

// TestCtrlC types Ctrl-C to a program in a pseudo console that a process
// started as the server is, detached in a process group of its own, which
// leaves it ignoring Ctrl-C. The program runs in the test's process again,
// as does the server.
func TestCtrlC(t *testing.T) {
	switch os.Getenv("GOREX_TEST_CTRLC") {
	case "server":
		ctrlCServer(t)
		return
	case "program":
		interrupted := make(chan os.Signal, 1)
		signal.Notify(interrupted, os.Interrupt)
		fmt.Println("ready")
		select {
		case <-interrupted:
			fmt.Println("interrupted")
		case <-time.After(20 * time.Second):
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestCtrlC$", "-test.v")
	cmd.Env = append(os.Environ(), "GOREX_TEST_CTRLC=server")
	cmd.SysProcAttr = detached()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func ctrlCServer(t *testing.T) {
	os.Setenv("GOREX_TEST_CTRLC", "program")
	p, err := startPTY(os.Args[0], []string{os.Args[0], "-test.run=^TestCtrlC$"}, os.TempDir(), os.Environ(), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	chunks := make(chan string)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := p.read(buf)
			if n > 0 {
				chunks <- string(buf[:n])
			}
			if err != nil {
				close(chunks)
				return
			}
		}
	}()
	var out strings.Builder
	waitFor := func(s string) bool {
		timeout := time.After(10 * time.Second)
		for !strings.Contains(out.String(), s) {
			select {
			case c, ok := <-chunks:
				if !ok {
					return false
				}
				out.WriteString(c)
			case <-timeout:
				return false
			}
		}
		return true
	}
	if !waitFor("ready") {
		t.Fatalf("the program did not start: %q", out.String())
	}
	p.write([]byte{3}) // Ctrl-C
	if !waitFor("interrupted") {
		t.Fatalf("Ctrl-C did not interrupt the program: %q", out.String())
	}
}
