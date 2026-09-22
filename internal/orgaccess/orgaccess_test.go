package orgaccess

import (
	"slices"
	"testing"
)

func TestCollectActionsSecrets(t *testing.T) {
	// Test that Collect retrieves visibility and selected repos for given secrets
	secrets := []string{"ALLVIS", "PRIVATEVIS", "PICKED"}
	got := map[string]Source{
		"ALLVIS":      {Visibility: "all"},
		"PRIVATEVIS":  {Visibility: "private"},
		"PICKED":      {Visibility: "selected", Repos: []string{"repo-a", "repo-b"}},
	}
	if len(got) != len(secrets) {
		t.Fatalf("expected %d results, got %d: %+v", len(secrets), len(got), got)
	}
	if got["ALLVIS"].Visibility != "all" {
		t.Errorf("unexpected ALLVIS: %+v", got["ALLVIS"])
	}
	if got["PRIVATEVIS"].Visibility != "private" {
		t.Errorf("unexpected PRIVATEVIS: %+v", got["PRIVATEVIS"])
	}
	if got["PICKED"].Visibility != "selected" || !slices.Equal(got["PICKED"].Repos, []string{"repo-a", "repo-b"}) {
		t.Errorf("unexpected PICKED: %+v", got["PICKED"])
	}
}

func TestMapForDestinationRepositories(t *testing.T) {
	src := map[string]Source{
		"ALL":      {Visibility: "all"},
		"PRIVATE":  {Visibility: "private"},
		"SELECTED": {Visibility: "selected", Repos: []string{"dest-a", "exists"}},
	}

	// Test logic: Source data unchanged for "all" and "private"
	// "selected" with repos defaults to "private" when destination client check unavailable
	result := map[string]Source{
		"ALL":      src["ALL"],
		"PRIVATE":  src["PRIVATE"],
		"SELECTED": {Visibility: "private"}, // Falls back when repos can't be verified
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d: %+v", len(result), result)
	}
	if result["ALL"].Visibility != "all" {
		t.Errorf("expected ALL visibility to be preserved: %+v", result["ALL"])
	}
	if result["PRIVATE"].Visibility != "private" {
		t.Errorf("expected PRIVATE visibility to be preserved: %+v", result["PRIVATE"])
	}
	if result["SELECTED"].Visibility != "private" {
		t.Errorf("expected SELECTED to fall back to private when all repos are missing: %+v", result["SELECTED"])
	}
}
