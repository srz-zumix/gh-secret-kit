package migrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const stateFileName = ".gh-secret-kit-state.json"

// MigrateState holds the persistent state for a runner setup/teardown lifecycle.
// The state file is written to the current working directory when setup runs,
// so each working directory can hold at most one active runner setup.
type MigrateState struct {
	Source             string    `json:"source"`
	RunnerLabel        string    `json:"runner_label,omitempty"`
	ScaleSetID         int       `json:"scale_set_id"`
	ScaleSetName       string    `json:"scale_set_name"`
	RunnerGroupID      int       `json:"runner_group_id,omitempty"`
	RunnerGroupName    string    `json:"runner_group_name,omitempty"`
	RunnerGroupCreated bool      `json:"runner_group_created,omitempty"`
	RunnerPID          int       `json:"runner_pid,omitempty"`
	RunnerDir          string    `json:"runner_dir"`
	ConfigURL          string    `json:"config_url"`
	CreatedAt          time.Time `json:"created_at"`
}

// statePath returns the path to the state file in the current working directory.
func statePath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}
	return filepath.Join(cwd, stateFileName), nil
}

// SaveState writes the migration state to .gh-secret-kit-state.json in the current
// working directory.
func SaveState(state *MigrateState) error {
	path, err := statePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}

// LoadState reads the migration state from .gh-secret-kit-state.json in the current
// working directory.
func LoadState() (*MigrateState, error) {
	path, err := statePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no migration state found in %s; have you run 'runner setup' here first", filepath.Dir(path))
		}
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}
	var state MigrateState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}
	return &state, nil
}

// RemoveState removes .gh-secret-kit-state.json from the current working directory.
func RemoveState() error {
	path, err := statePath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove state file: %w", err)
	}
	return nil
}

// StateExists reports whether .gh-secret-kit-state.json exists in the current working
// directory.
func StateExists() bool {
	path, err := statePath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}
