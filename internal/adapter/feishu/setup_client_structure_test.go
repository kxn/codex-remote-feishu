package feishu

import (
	"os"
	"strings"
	"testing"
)

func TestSetupClientOwnsSetupAPIMethods(t *testing.T) {
	files := []string{
		"connection_status.go",
		"scopes.go",
		"app_autoconfig.go",
	}
	source := readStructureTestFiles(t, files...)
	for _, method := range []string{
		"GetLongConnectionStatus",
		"GetBotInfo",
		"ListAppScopes",
		"PlanAppAutoConfig",
		"ApplyAppAutoConfig",
		"PublishAppAutoConfig",
	} {
		if !strings.Contains(source, "func (c *SetupClient) "+method+"(") {
			t.Fatalf("SetupClient.%s is missing; setup/admin API helpers should live behind the setup facade", method)
		}
	}
}

func TestLegacySetupFunctionsAreThinSetupClientBridges(t *testing.T) {
	cases := []struct {
		file string
		fn   string
		call string
	}{
		{"connection_status.go", "GetLongConnectionStatus", "NewSetupClient(SetupClientConfigFromLiveGatewayConfig(cfg)).GetLongConnectionStatus(ctx)"},
		{"connection_status.go", "GetBotInfo", "NewSetupClient(SetupClientConfigFromLiveGatewayConfig(cfg)).GetBotInfo(ctx)"},
		{"scopes.go", "ListAppScopes", "NewSetupClient(SetupClientConfigFromLiveGatewayConfig(cfg)).ListAppScopes(ctx)"},
		{"app_autoconfig.go", "PlanAppAutoConfig", "NewSetupClient(SetupClientConfigFromLiveGatewayConfig(cfg)).PlanAppAutoConfig(ctx, manifest, policy)"},
		{"app_autoconfig.go", "ApplyAppAutoConfig", "NewSetupClient(SetupClientConfigFromLiveGatewayConfig(cfg)).ApplyAppAutoConfig(ctx, manifest, policy)"},
		{"app_autoconfig.go", "PublishAppAutoConfig", "NewSetupClient(SetupClientConfigFromLiveGatewayConfig(cfg)).PublishAppAutoConfig(ctx, manifest, policy, req)"},
	}
	for _, tc := range cases {
		data, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatalf("read %s: %v", tc.file, err)
		}
		body := functionBodyForFeishuStructureTest(t, string(data), tc.fn)
		if !strings.Contains(compactStructureSource(body), compactStructureSource(tc.call)) {
			t.Fatalf("%s:%s is not a thin SetupClient bridge", tc.file, tc.fn)
		}
	}
}

func readStructureTestFiles(t *testing.T, files ...string) string {
	t.Helper()
	var builder strings.Builder
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		builder.Write(data)
		builder.WriteByte('\n')
	}
	return builder.String()
}

func functionBodyForFeishuStructureTest(t *testing.T, source, name string) string {
	t.Helper()
	needle := "func " + name + "("
	start := strings.Index(source, needle)
	if start < 0 {
		t.Fatalf("function %s not found", name)
	}
	open := strings.Index(source[start:], "{")
	if open < 0 {
		t.Fatalf("function %s has no body", name)
	}
	pos := start + open
	depth := 0
	for i := pos; i < len(source); i++ {
		switch source[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return source[pos : i+1]
			}
		}
	}
	t.Fatalf("function %s body is not balanced", name)
	return ""
}

func compactStructureSource(source string) string {
	return strings.Join(strings.Fields(source), "")
}
