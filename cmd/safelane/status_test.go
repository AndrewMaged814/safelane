package main

import (
	"bytes"
	"testing"
)

func TestNoArgumentsPrintsPrimaryWorkflowHelpAndSucceeds(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("run exit = %d, want 0", code)
	}
	for _, command := range [][]byte{[]byte("setup"), []byte("doctor"), []byte("release")} {
		if !bytes.Contains(stdout.Bytes(), command) {
			t.Fatalf("help does not include %q:\n%s", command, stdout.String())
		}
	}
}

func TestDeletedCommandsAreOrdinaryUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"init"}, {"rollout", "start"}, {"status"}, {"proof", "rel_01"}, {"release", "inspect"}, {"release", "--pr", "1"}, {"setup", "apply", "--proposal", "proposal.json", "--yes"}, {"demo", "up"}, {"demo", "down"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Errorf("%v exit = %d, want 2", args, code)
		}
	}
}

func TestSetupHelpTeachesInspectPlanApply(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"setup", "--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run exit = %d, stderr = %s", code, stderr.String())
	}
	for _, command := range [][]byte{[]byte("inspect"), []byte("plan"), []byte("apply")} {
		if !bytes.Contains(stdout.Bytes(), command) {
			t.Fatalf("setup help does not include %q:\n%s", command, stdout.String())
		}
	}
}
