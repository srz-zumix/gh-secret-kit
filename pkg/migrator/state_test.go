package migrator

import (
	"testing"
	"time"
)

func useTempCwd(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
}

func testState(configURL, runnerLabel string, scaleSetID int) *MigrateState {
	return &MigrateState{
		Source:          configURL,
		RunnerLabel:     runnerLabel,
		ScaleSetID:      scaleSetID,
		ScaleSetName:    runnerLabel,
		RunnerGroupID:   42,
		RunnerGroupName: "custom-group",
		RunnerDir:       "runner",
		ConfigURL:       configURL,
		CreatedAt:       time.Now(),
	}
}

func TestSaveAndLoadState(t *testing.T) {
	useTempCwd(t)

	state := testState("https://github.com/owner/repo", "gh-secret-kit-migrate", 101)
	if err := SaveState(state); err != nil {
		t.Fatalf("SaveState returned error: %v", err)
	}

	loaded, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState returned error: %v", err)
	}
	if loaded.ScaleSetID != state.ScaleSetID {
		t.Fatalf("ScaleSetID = %d, want %d", loaded.ScaleSetID, state.ScaleSetID)
	}
	if loaded.RunnerLabel != state.RunnerLabel {
		t.Fatalf("RunnerLabel = %q, want %q", loaded.RunnerLabel, state.RunnerLabel)
	}
	if loaded.RunnerGroupID != state.RunnerGroupID {
		t.Fatalf("RunnerGroupID = %d, want %d", loaded.RunnerGroupID, state.RunnerGroupID)
	}
	if loaded.RunnerGroupName != state.RunnerGroupName {
		t.Fatalf("RunnerGroupName = %q, want %q", loaded.RunnerGroupName, state.RunnerGroupName)
	}
}

func TestStateExistsAfterSave(t *testing.T) {
	useTempCwd(t)

	if StateExists() {
		t.Fatal("StateExists returned true before SaveState")
	}
	state := testState("https://github.com/owner/repo", "gh-secret-kit-migrate", 101)
	if err := SaveState(state); err != nil {
		t.Fatalf("SaveState returned error: %v", err)
	}
	if !StateExists() {
		t.Fatal("StateExists returned false after SaveState")
	}
}

func TestRemoveState(t *testing.T) {
	useTempCwd(t)

	state := testState("https://github.com/owner/repo", "gh-secret-kit-migrate", 101)
	if err := SaveState(state); err != nil {
		t.Fatalf("SaveState returned error: %v", err)
	}
	if err := RemoveState(); err != nil {
		t.Fatalf("RemoveState returned error: %v", err)
	}
	if StateExists() {
		t.Fatal("StateExists returned true after RemoveState")
	}
}

func TestLoadStateNotFoundError(t *testing.T) {
	useTempCwd(t)

	if _, err := LoadState(); err == nil {
		t.Fatal("LoadState returned nil error when no state file exists")
	}
}

func TestStateIsIsolatedToCwd(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	t.Chdir(dir1)
	state1 := testState("https://github.com/owner/one", "gh-secret-kit-migrate", 101)
	if err := SaveState(state1); err != nil {
		t.Fatalf("SaveState(dir1) returned error: %v", err)
	}

	t.Chdir(dir2)
	if StateExists() {
		t.Fatal("StateExists returned true in a different directory")
	}
	if _, err := LoadState(); err == nil {
		t.Fatal("LoadState returned nil error in a directory with no state")
	}

	state2 := testState("https://github.com/owner/two", "gh-secret-kit-migrate", 202)
	if err := SaveState(state2); err != nil {
		t.Fatalf("SaveState(dir2) returned error: %v", err)
	}

	t.Chdir(dir1)
	loaded, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState(dir1) returned error: %v", err)
	}
	if loaded.ScaleSetID != state1.ScaleSetID {
		t.Fatalf("dir1 ScaleSetID = %d, want %d", loaded.ScaleSetID, state1.ScaleSetID)
	}
}
