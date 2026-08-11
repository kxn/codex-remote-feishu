package daemon

import "testing"

func TestCancellablePickerRuntimeCompletesAndDeletes(t *testing.T) {
	runtimes := map[string]*cancellablePickerRuntime{}

	key := beginCancellablePickerRuntime(runtimes, " surface-1 ", " picker-1 ", func() {})

	if key != "surface-1::picker-1" {
		t.Fatalf("runtime key = %q, want trimmed surface/picker key", key)
	}
	if _, ok := runtimes[key]; !ok {
		t.Fatalf("runtime %q was not stored", key)
	}
	if cancelled := finishCancellablePickerRuntime(runtimes, key); cancelled {
		t.Fatal("uncancelled runtime finished as cancelled")
	}
	if _, ok := runtimes[key]; ok {
		t.Fatalf("runtime %q survived finish", key)
	}
	if cancelled := finishCancellablePickerRuntime(runtimes, key); cancelled {
		t.Fatal("missing runtime finished as cancelled")
	}
}

func TestCancellablePickerRuntimeSkipsEmptySurfaceAndPicker(t *testing.T) {
	runtimes := map[string]*cancellablePickerRuntime{}

	key := beginCancellablePickerRuntime(runtimes, "  ", "\t", func() {})

	if key != "" {
		t.Fatalf("empty surface/picker key = %q, want empty", key)
	}
	if len(runtimes) != 0 {
		t.Fatalf("empty surface/picker created runtimes: %#v", runtimes)
	}
	if cancelled := finishCancellablePickerRuntime(runtimes, "  "); cancelled {
		t.Fatal("blank key finished as cancelled")
	}
}

func TestCancellablePickerRuntimeCancelMarksRuntimeCancelled(t *testing.T) {
	runtimes := map[string]*cancellablePickerRuntime{}
	cancelCalls := 0
	key := beginCancellablePickerRuntime(runtimes, "surface-1", "picker-1", func() {
		cancelCalls++
	})

	cancelCancellablePickerRuntime(runtimes, " surface-1 ", " picker-1 ")
	cancelCancellablePickerRuntime(runtimes, "surface-1", "picker-1")

	if cancelCalls != 2 {
		t.Fatalf("cancel calls = %d, want existing repeated-cancel behavior", cancelCalls)
	}
	if cancelled := finishCancellablePickerRuntime(runtimes, key); !cancelled {
		t.Fatal("cancelled runtime finished as uncancelled")
	}
	if _, ok := runtimes[key]; ok {
		t.Fatalf("cancelled runtime %q survived finish", key)
	}
}
