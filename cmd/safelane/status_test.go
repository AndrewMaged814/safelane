package main

import (
	"bytes"
	"testing"
)

func TestStatusIsRegisteredAsATopLevelCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 2 {
		t.Fatalf("run exit = %d, want usage exit 2", code)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("status     show one live rollout or list open releases")) {
		t.Fatalf("top-level usage does not include status:\n%s", stderr.String())
	}
}
