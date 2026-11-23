package workupgrade

import (
	"context"
	"errors"
	"os"
	"testing"
)

type recordingMaintenance struct {
	steps []string
	block bool
}

func (m *recordingMaintenance) BeginAndDrainPrivate(context.Context, Record) error {
	m.steps = append(m.steps, "begin")
	return nil
}
func (m *recordingMaintenance) BeforeInstall(context.Context, Record) error {
	m.steps = append(m.steps, "before-install")
	if m.block {
		return errors.New("private carrier has no stop evidence")
	}
	return nil
}
func (m *recordingMaintenance) AfterVerified(context.Context, Record) error {
	m.steps = append(m.steps, "verified")
	return nil
}
func (m *recordingMaintenance) AfterTeamRestart(context.Context, Record) error {
	m.steps = append(m.steps, "restored")
	return nil
}

func TestMaintenanceBlocksInstallerUntilSharedQuiescence(t *testing.T) {
	c, req, log := upgradeFixture(t)
	m := &recordingMaintenance{block: true}
	c.config.Maintenance = m
	if _, err := c.Request(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if err := c.Run(t.Context(), 1); err == nil {
		t.Fatal("maintenance did not block installation")
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("installer ran before shared quiescence: %v", err)
	}
	m.block = false
	if err := c.Run(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	want := []string{"begin", "before-install", "begin", "before-install", "verified", "restored"}
	if len(m.steps) != len(want) {
		t.Fatalf("lifecycle: %v", m.steps)
	}
	for i := range want {
		if m.steps[i] != want[i] {
			t.Fatalf("lifecycle: %v", m.steps)
		}
	}
}
