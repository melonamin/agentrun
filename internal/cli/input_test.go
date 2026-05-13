package cli

import (
	"strings"
	"testing"
)

func TestReadTextPromptArgs(t *testing.T) {
	got, err := readTextPrompt([]string{"hello", "world"}, strings.NewReader("ignored"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello world" {
		t.Fatalf("got %q", got)
	}
}

func TestReadTextPromptStdin(t *testing.T) {
	got, err := readTextPrompt(nil, strings.NewReader("hello\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestReadStreamMessages(t *testing.T) {
	in := strings.NewReader(`{"type":"user","message":{"role":"user","content":"hello"}}
{"type":"assistant","message":{"content":"ignored"}}
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"second"}]}}
`)
	msgs, err := readStreamMessages(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0].Text != "hello" || msgs[1].Text != "second" {
		t.Fatalf("msgs %#v", msgs)
	}
}
