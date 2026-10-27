//go:build darwin || linux || windows

package rex

import "testing"

func TestIsReply(t *testing.T) {
	for in, want := range map[string]bool{
		"\x1b]11;rgb:2828/2828/2828\x1b\\\x1b[2;1R": true, // what gh's query got
		"\x1b]11;rgb:0/0/0\x07":                     true,
		"\x1b[?62;22c":                              true,
		"\x1b[0n":                                   true,
		"\x1b[?2026;2$y":                            true,
		"\x1b[?1u":                                  true,
		"\x1bP>|ghostty\x1b\\":                      true,
		"":                                          false,
		"ls\r":                                      false,
		"\x1b[A":                                    false,
		"\x1b[I":                                    false, // focus in
		"\x1b[97;5u":                                false, // kitty key
		"\x1b[<0;10;5M":                             false, // mouse
		"\x1b]":                                     false, // Alt+]
		"\x1b]11;rgb:0/0/0\x07l":                    false,
	} {
		if got := isReply([]byte(in)); got != want {
			t.Errorf("isReply(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestRepliesFromOneWindow checks that, with two windows on a session,
// both of whose terminals answer a query, the program gets one answer:
// the window typed in last's.
func TestRepliesFromOneWindow(t *testing.T) {
	s := &session{}
	a, b := &attached{}, &attached{}
	reply := []byte("\x1b]11;rgb:2828/2828/2828\x1b\\\x1b[2;1R")
	steps := []struct {
		from *attached
		in   []byte
		want bool
	}{
		{b, reply, true}, // none typed yet: the first to answer
		{a, reply, false},
		{a, []byte("gh repo clone x\r"), true},
		{a, reply, true},
		{b, reply, false},
		{b, []byte("\x1b[I"), true}, // focused, b is looked at now
		{b, reply, true},
		{a, reply, false},
		{nil, reply, true},
	}
	for i, st := range steps {
		if got := s.takes(st.from, st.in); got != st.want {
			t.Fatalf("step %d: takes %q = %v, want %v", i, st.in, got, st.want)
		}
	}
}
