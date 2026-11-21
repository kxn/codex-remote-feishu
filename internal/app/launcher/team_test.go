package launcher

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"
)

func TestTeamUsesDedicatedRunnerAndPreservesArguments(t *testing.T) {
	args := []string{"task.assign", "--run-ref", "run-one", "--stdin"}
	decision, err := Detect(append([]string{"team"}, args...))
	if err != nil || decision.Role != RoleTeam || !reflect.DeepEqual(decision.Args, args) {
		t.Fatalf("decision %#v, %v", decision, err)
	}
	called := 0
	var out, stderr bytes.Buffer
	code := Main(Options{Args: append([]string{"team"}, args...), Stdout: &out, Stderr: &stderr, Runners: RunnerSet{RunTeam: func(_ context.Context, got []string, _ io.Reader, _ io.Writer, _ io.Writer) error {
		called++
		if !reflect.DeepEqual(got, args) {
			t.Errorf("args %v", got)
		}
		return nil
	}}})
	if code != 0 || called != 1 {
		t.Fatalf("team dispatch exit=%d calls=%d stderr=%s", code, called, stderr.String())
	}
}

func TestTeamHelpWorksWithoutCredentialEnvironment(t *testing.T) {
	var out, stderr bytes.Buffer
	code := Main(Options{Args: []string{"team", "--help"}, Stdout: &out, Stderr: &stderr})
	if code != 0 || !bytes.Contains(out.Bytes(), []byte("task.assign")) {
		t.Fatalf("help exit=%d out=%s stderr=%s", code, out.String(), stderr.String())
	}
}
