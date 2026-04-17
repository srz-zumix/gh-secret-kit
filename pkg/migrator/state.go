package migrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const stateFileName = "migrate-state.json"

// MigrateState holds the persistent state for a runner setup/teardown lifecycle
type MigrateState struct {
	Source             string    `json:"source"`
	ScaleSetID         int       `json:"scale_set_id"`
	ScaleSetName       string    `json:"scale_set_name"`
	RunnerPID          int       `json:"runner_pid,omitempty"`
	RunnerDir          string    `json:"runner_dir"`
	ConfigURL          string    `json:"config_url"`
	RunnerGroupName    string    `json:"runner_group_name,omitempty"`
	RunnerGroupCreated bool      `json:"runner_group_created,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
}

// stateDir returns the directory for storing state files
func stateDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user config directory: %w", err)
	}
	return filepath.Join(configDir, "gh-secret-kit"), nil
}

// statePath returns the full path to the state file
func statePath() (string, error) {
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, stateFileName), nil
}

// SaveState saves the migration state to a JSON file
func SaveState(state *MigrateState) error {
	dir, err := stateDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

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

// LoadState loads the migration state from the state file
func LoadState() (*MigrateState, error) {
	path, err := statePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no migration state found; have you run 'runner setup' first")
		}
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state MigrateState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}

	return &state, nil
}

// RemoveState removes the state file
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

// StateExists checks if a migration state file exists
func StateExists() bool {
	path, err := statePath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}
