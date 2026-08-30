package kubernetes

import (
	"strings"
	"testing"
)

func TestLineEmitterSplitsAndFlushes(t *testing.T) {
	var got []string
	em := &lineEmitter{emit: func(s string) { got = append(got, s) }}
	if _, err := em.Write([]byte("hello\nwor")); err != nil {
		t.Fatal(err)
	}
	if _, err := em.Write([]byte("ld\n")); err != nil {
		t.Fatal(err)
	}
	em.Flush()
	if strings.Join(got, ",") != "hello,world" {
		t.Fatalf("got %v", got)
	}
}

func TestLineEmitterStripsCR(t *testing.T) {
	var got []string
	em := &lineEmitter{emit: func(s string) { got = append(got, s) }}
	_, _ = em.Write([]byte("one\r\n"))
	if len(got) != 1 || got[0] != "one" {
		t.Fatalf("got %v", got)
	}
}

func TestLineEmitterDropsEmpty(t *testing.T) {
	var got []string
	em := &lineEmitter{emit: func(s string) { got = append(got, s) }}
	_, _ = em.Write([]byte("\n\nkeep\n"))
	if len(got) != 1 || got[0] != "keep" {
		t.Fatalf("got %v", got)
	}
}
