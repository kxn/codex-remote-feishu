package workcmd

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRequestStrictJSONAndExplicitRun(t *testing.T) {
	valid := `{"name":"context","run_ref":"run-one","payload":{}}`
	if _, err := DecodeRequest([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"unknown-command":    `{"name":"feishu.call","run_ref":"run-one","payload":{}}`,
		"missing-run":        `{"name":"context","payload":{}}`,
		"unknown-field":      `{"name":"context","run_ref":"run-one","payload":{},"actor":"admin"}`,
		"case-alias":         `{"name":"context","Name":"task.assign","run_ref":"run-one","payload":{}}`,
		"duplicate-envelope": `{"name":"context","name":"task.assign","run_ref":"run-one","payload":{}}`,
		"duplicate-payload":  `{"name":"task.assign","run_ref":"run-one","payload":{"key":"a","key":"b"}}`,
		"trailing-value":     valid + ` {}`,
		"null-payload":       `{"name":"context","run_ref":"run-one","payload":null}`,
		"nested-depth":       `{"name":"context","run_ref":"run-one","payload":{"deep":` + strings.Repeat("[", 65) + `0` + strings.Repeat("]", 65) + `}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeRequest([]byte(body)); err == nil {
				t.Fatalf("accepted %s", body)
			}
		})
	}
}

func TestPayloadUsesActualDTOAndRegistryIsIsolated(t *testing.T) {
	var input struct {
		Title string `json:"title"`
	}
	if err := DecodePayload(json.RawMessage(`{"title":"Task"}`), &input); err != nil || input.Title != "Task" {
		t.Fatalf("decode %v", err)
	}
	if err := DecodePayload(json.RawMessage(`{"title":"Task","scope_id":"other"}`), &input); err == nil {
		t.Error("unknown DTO field accepted")
	}
	list := List()
	originalName := list[0].Name
	list[0].Name = "tampered"
	for i := range list {
		if list[i].Name == "task.assign" {
			list[i].InputFields[0].Name = "tampered"
		}
	}
	if _, ok := Lookup(originalName); !ok {
		t.Error("caller mutated registry")
	}
	command, _ := Lookup("task.assign")
	if command.InputFields[0].Name != "key" {
		t.Error("caller mutated input descriptions")
	}
}

func TestDTORejectsCaseAliasesAndNestedUnknownFields(t *testing.T) {
	var input struct {
		Key   string `json:"key"`
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	for _, payload := range []string{`{"key":"first","KEY":"second"}`, `{"items":[{"name":"one","Name":"two"}]}`, `{"items":[{"name":"one","unknown":true}]}`} {
		if err := DecodePayload(json.RawMessage(payload), &input); err == nil {
			t.Errorf("accepted DTO alias/unknown field: %s", payload)
		}
	}
}
