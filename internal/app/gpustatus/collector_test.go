package gpustatus

import "testing"

func TestParseGPUQuery(t *testing.T) {
	t.Parallel()

	gpus, err := Parse([]byte("0, NVIDIA RTX A6000, 45, 0, 10, 49140, 17.2, 300.0\n1, NVIDIA RTX A6000, 63, 17, 8221, 49140, 115.5, 300.0\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(gpus) != 2 {
		t.Fatalf("Parse() GPUs = %d, want 2", len(gpus))
	}
	if gpus[1].Index != 1 || gpus[1].Name != "NVIDIA RTX A6000" || gpus[1].MemoryUsedMiB != 8221 || gpus[1].PowerDrawW != 115.5 {
		t.Fatalf("Parse() GPU 1 = %#v", gpus[1])
	}
}

func TestParseGPUQueryAllowsUnsupportedPower(t *testing.T) {
	t.Parallel()

	gpus, err := Parse([]byte("0, NVIDIA GPU, 40, 2, 100, 1000, N/A, [Not Supported]\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if gpus[0].PowerDrawW != 0 || gpus[0].PowerLimitW != 0 {
		t.Fatalf("Parse() unsupported power = %#v", gpus[0])
	}
}

func TestParseGPUQueryRejectsMalformedRows(t *testing.T) {
	t.Parallel()

	if _, err := Parse([]byte("0, NVIDIA GPU, 40\n")); err == nil {
		t.Fatal("Parse() error = nil, want malformed-row error")
	}
}
