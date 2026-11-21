package workprovision

import (
	"context"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
)

func TestSelectedDriveFolderIdentityIsExplicit(t *testing.T) {
	for _, value := range []string{"folderToken", "drive:folderToken", "https://tenant.feishu.cn/drive/folder/folderToken?from=chat"} {
		token, err := selectedFolderToken(value)
		if err != nil || token != "folderToken" {
			t.Fatalf("%q: %q %v", value, token, err)
		}
	}
	for _, value := range []string{"https://tenant.feishu.cn/docx/notFolder", "drive:", "../folder", "folder name"} {
		if _, err := selectedFolderToken(value); err == nil {
			t.Fatalf("invalid folder accepted: %q", value)
		}
	}
}
func TestProvisionExecutionRejectsLocationWithoutSavedHumanChoice(t *testing.T) {
	f := newFixture(t)
	state, err := f.service.load(context.Background(), f.operationID)
	if err != nil {
		t.Fatal(err)
	}
	state.request.LocationIDs = []string{"model-guessed-directory"}
	state.request.DefaultLocationID = "model-guessed-directory"
	if err = f.service.prepareExecution(context.Background(), &state); err == nil {
		t.Fatal("model-guessed existing directory reached execution preparer")
	}
	if f.prepared != 0 {
		t.Fatalf("preparer called %d", f.prepared)
	}
	if err = f.store.Within(context.Background(), func(tx *workstore.Tx) error {
		steps, err := tx.ListProvisionSteps(f.operationID)
		if err != nil {
			return err
		}
		for _, step := range steps {
			if step.StepID == "execution.prepare" {
				t.Fatal("unauthorized directory saved as effect intent")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Empty location input is represented faithfully, not a guessed default.
	state.request.LocationIDs = nil
	state.request.DefaultLocationID = ""
	if err = f.service.prepareExecution(context.Background(), &state); err != nil {
		t.Fatal(err)
	}
	if f.prepared != 1 {
		t.Fatal("isolated scratch preparation missing")
	}
}
