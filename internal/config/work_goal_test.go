package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkGoalBudgetDefaultsAndRoundTripPreserveCredentials(t *testing.T) {
	cfg := DefaultAppConfig()
	cfg.Feishu.Apps = []FeishuAppConfig{{ID: "gateway", AppID: "app", AppSecret: "preserve-credential"}}
	cfg.Work.DefaultGoalTokenBudget = 54321
	path := filepath.Join(t.TempDir(), "config.json")
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := readConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Work.DefaultGoalTokenBudget != 54321 || loaded.Feishu.Apps[0].AppSecret != "preserve-credential" {
		t.Fatal("new goal policy changed explicit budget or credentials")
	}
	cfg.Work.DefaultGoalTokenBudget = 0
	raw, _ = json.Marshal(cfg)
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err = readConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Work.DefaultGoalTokenBudget != 100000 {
		t.Fatal("omitted goal budget did not receive documented default")
	}
	cfg.Work.DefaultGoalTokenBudget = -1
	raw, _ = json.Marshal(cfg)
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = readConfigFile(path); err == nil {
		t.Fatal("negative default goal budget accepted")
	}
}
