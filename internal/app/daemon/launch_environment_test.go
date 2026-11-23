package daemon

import (
	"reflect"
	"testing"
)

func TestStableEffectiveLaunchEnvironmentPreservesLastValue(t *testing.T) {
	a := []string{"Z=old", "A=one", "Z=new", "EMPTY="}
	b := []string{"EMPTY=", "Z=old", "Z=new", "A=one"}
	want := []string{"A=one", "EMPTY=", "Z=new"}
	if got := stableEffectiveLaunchEnvironment(a); !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	if got := stableEffectiveLaunchEnvironment(b); !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	if reflect.DeepEqual(stableEffectiveLaunchEnvironment([]string{"Z=new", "Z=old"}), want) {
		t.Fatal("changed last value treated as same")
	}
}
