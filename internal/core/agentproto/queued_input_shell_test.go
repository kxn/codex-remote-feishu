package agentproto

import "testing"

func TestQueuedInputShellPayloadPreservesOriginalBytes(t *testing.T) {
	inputs := []Input{{Type: InputText, Text: ""}, {Type: InputText, Text: "  first "}, {Type: InputText, Text: ""}, {Type: InputText, Text: "second\n"}, {Type: InputLocalImage, Path: " /tmp/a b.png ", MIMEType: " image/png "}, {Type: InputRemoteImage, Path: " ", URL: " https://example.invalid/image ", MIMEType: " image/jpeg "}}
	got, ok, err := QueuedInputShellPayload(inputs)
	want := "<queued_input_bundle_v1>\n" + `{
  "schema": "queued_input_bundle.v1",
  "text": "  first \nsecond\n",
  "attachments": [
    {
      "ref": "att-1",
      "type": "image",
      "path": "/tmp/a b.png",
      "mime_type": "image/png"
    },
    {
      "ref": "att-2",
      "type": "image",
      "path": "https://example.invalid/image",
      "mime_type": "image/jpeg"
    }
  ]
}` + "\n</queued_input_bundle_v1>\n"
	if err != nil || !ok || got != want {
		t.Fatalf("original wire bytes changed: %q %v %v", got, ok, err)
	}
	for _, input := range []Input{{Type: InputLocalImage, Path: " "}, {Type: InputRemoteImage, Path: " ", URL: " "}} {
		if got, ok, err := QueuedInputShellPayload([]Input{input}); got != "" || ok || err == nil {
			t.Fatalf("missing image reference accepted: %q %v %v", got, ok, err)
		}
	}
	for _, inputs := range [][]Input{nil, {{Type: InputText, Text: " \n "}}, {{Type: InputText, Text: ""}}} {
		if got, ok, err := QueuedInputShellPayload(inputs); got != "" || ok || err != nil {
			t.Fatalf("empty input encoded: %q %v %v", got, ok, err)
		}
	}
}
