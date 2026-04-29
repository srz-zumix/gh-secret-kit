package runner

import (
	"testing"

	"github.com/cli/go-gh/v2/pkg/repository"
)

func TestStateSourceStringIncludesHost(t *testing.T) {
	repo := repository.Repository{Host: "ghe.example.com", Owner: "owner", Name: "repo"}
	if got := stateSourceString(repo); got != "ghe.example.com/owner/repo" {
		t.Fatalf("stateSourceString() = %q, want %q", got, "ghe.example.com/owner/repo")
	}
}

func TestResolveStateSourceRepoWithHostRepo(t *testing.T) {
	repo, err := resolveStateSourceRepo("ghe.example.com/owner/repo")
	if err != nil {
		t.Fatalf("resolveStateSourceRepo() returned error: %v", err)
	}
	if repo.Host != "ghe.example.com" || repo.Owner != "owner" || repo.Name != "repo" {
		t.Fatalf("repo = %#v, want host=%q owner=%q name=%q", repo, "ghe.example.com", "owner", "repo")
	}
}

func TestResolveStateSourceRepoWithHostOwner(t *testing.T) {
	repo, err := resolveStateSourceRepo("ghe.example.com/owner")
	if err != nil {
		t.Fatalf("resolveStateSourceRepo() returned error: %v", err)
	}
	if repo.Host != "ghe.example.com" || repo.Owner != "owner" || repo.Name != "" {
		t.Fatalf("repo = %#v, want host=%q owner=%q name=%q", repo, "ghe.example.com", "owner", "")
	}
}
