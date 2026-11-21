package workservice

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

func TestCommandDiscoveryMatchesExecutableDTOs(t *testing.T) {
	s, token, run := runningService(t)
	dtos := map[string]any{"context": struct{}{}, "task.assign": AssignTaskRequest{}, "task.revise": ReviseTaskRequest{}, "message.send": SendMessageRequest{}, "outcome.report": ReportOutcomeRequest{}, "project.create": CreateProjectRequest{}, "content.read": ContentReadRequest{}, "bootstrap.context": struct{}{}, "message.publish": PublishMessageRequest{}}
	dtos["control.consume"] = ControlConsumeRequest{}
	dtos["bootstrap.location.pick"] = BootstrapPickLocationRequest{}
	dtos["bootstrap.team.resolve"] = BootstrapResolveTeamRequest{}
	dtos["execution.locations"] = struct{}{}
	dtos["execution.location.use"] = ExecutionLocationRequest{}
	for name, dto := range extendedCommandDTOs() {
		dtos[name] = dto
	}
	for _, command := range workcmd.List() {
		t.Run(command.Name, func(t *testing.T) {
			dto, ok := dtos[command.Name]
			if !ok {
				t.Fatal("discovery command has no executable DTO")
			}
			typ := reflect.TypeOf(dto)
			fields := map[string]bool{}
			for i := 0; i < typ.NumField(); i++ {
				fields[strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]] = true
			}
			for _, field := range command.InputFields {
				if !fields[field.Name] {
					t.Fatalf("help field %s not in DTO", field.Name)
				}
				delete(fields, field.Name)
			}
			if len(fields) > 0 {
				t.Fatalf("DTO fields not discoverable: %v", fields)
			}
			ref := run
			if command.CredentialPurpose == "bootstrap" {
				ref = ""
			}
			_, err := s.Execute(context.Background(), token, workcmd.DispatchRequest{Name: command.Name, RunRef: ref, Payload: json.RawMessage(`{"unknown_field":true}`)})
			var commandErr *workcmd.Error
			if !errors.As(err, &commandErr) || commandErr.Code != "invalid_argument" {
				t.Fatalf("typed decoding missing: %v", err)
			}
			delete(dtos, command.Name)
		})
	}
	if len(dtos) > 0 {
		t.Fatalf("undiscoverable commands: %v", dtos)
	}
}
