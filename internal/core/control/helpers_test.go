package control

import "testing"

func TestNormalizeRequestOptionID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "accept", want: "accept"},
		{input: "allow-this-session", want: "acceptForSession"},
		{input: " reject ", want: "decline"},
		{input: "Tell Codex What To Do Differently", want: "captureFeedback"},
		{input: "custom-option", want: "custom-option"},
	}
	for _, tc := range tests {
		if got := NormalizeRequestOptionID(tc.input); got != tc.want {
			t.Fatalf("NormalizeRequestOptionID(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestRequestFeedbackActionLabel(t *testing.T) {
	if got := RequestFeedbackActionLabel("codex"); got != "告诉 Codex 怎么改" {
		t.Fatalf("codex feedback label = %q", got)
	}
	if got := RequestFeedbackActionLabel("claude"); got != "告诉 Claude 怎么改" {
		t.Fatalf("claude feedback label = %q", got)
	}
}

func TestShortenThreadID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "thread-abc-12345678", want: "abc…5678"},
		{input: "thread-abc-abc", want: "abc"},
		{input: "short-id", want: "id"},
		{input: "12345678901", want: "45678901"},
	}
	for _, tc := range tests {
		if got := ShortenThreadID(tc.input); got != tc.want {
			t.Fatalf("ShortenThreadID(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestFeishuCommandFormWithDefaultClones(t *testing.T) {
	form := FeishuCommandFormWithDefault(FeishuCommandModel, "gpt-5.4")
	if form == nil {
		t.Fatal("expected form")
	}
	if form.Field.DefaultValue != "gpt-5.4" {
		t.Fatalf("default value = %q, want gpt-5.4", form.Field.DefaultValue)
	}

	original, ok := FeishuCommandForm(FeishuCommandModel)
	if !ok || original == nil {
		t.Fatal("expected original form")
	}
	if original.Field.DefaultValue != "" {
		t.Fatalf("original form was mutated: %#v", original)
	}
}
