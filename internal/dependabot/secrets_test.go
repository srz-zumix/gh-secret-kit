package dependabot

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	check "testing"

	"github.com/google/go-github/v90/github"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"golang.org/x/crypto/nacl/box"
)

func TestDependabotSecretPagination(t *check.T) {
	for _, org := range []bool{false, true} {
		t.Run(fmt.Sprintf("org=%v", org), func(t *check.T) {
			pages := 0
			path := "/repos/owner/repo/dependabot/secrets"
			if org {
				path = "/orgs/owner/dependabot/secrets"
			}
			client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
				pages++
				if r.Method != "GET" || r.URL.Path != path || r.URL.Query().Get("per_page") != "100" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
				}
				if pages == 1 {
					w.Header().Set("Link", fmt.Sprintf(`<http://%s/api/v3%s?page=2>; rel="next"`, r.Host, path))
					fmt.Fprint(w, `{"total_count":2,"secrets":[{"name":"FIRST"}]}`)
					return
				}
				if r.URL.Query().Get("page") != "2" {
					t.Error("next page not requested")
				}
				fmt.Fprint(w, `{"total_count":2,"secrets":[{"name":"SECOND"}]}`)
			})
			var secrets []*github.Secret
			var err error
			if org {
				secrets, err = listDependabotOrgSecrets(context.Background(), client, sourceRepo)
			} else {
				secrets, err = listDependabotRepoSecrets(context.Background(), client, sourceRepo)
			}
			if err != nil || pages != 2 || len(secrets) != 2 || secrets[1].GetName() != "SECOND" {
				t.Fatalf("secrets=%v pages=%d err=%v", secrets, pages, err)
			}
		})
	}
}

func TestDependabotSecretEncryptionAndDeletion(t *check.T) {
	public, private, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wrote, deleted := false, false
	const value = "fixture-destination-value"
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/owner/repo/dependabot/secrets/public-key":
			_ = json.NewEncoder(w).Encode(github.PublicKey{KeyID: github.Ptr("key-id"), Key: github.Ptr(base64.StdEncoding.EncodeToString(public[:]))})
		case "PUT /repos/owner/repo/dependabot/secrets/TOKEN":
			var body github.SecretRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			encrypted, err := base64.StdEncoding.DecodeString(body.EncryptedValue)
			if err != nil {
				t.Error(err)
			}
			plain, ok := box.OpenAnonymous(nil, encrypted, public, private)
			if !ok || string(plain) != value || body.KeyID != "key-id" || strings.Contains(body.EncryptedValue, value) {
				t.Error("incorrect encrypted secret payload")
			}
			wrote = true
			w.WriteHeader(http.StatusCreated)
		case "DELETE /repos/owner/repo/dependabot/secrets/TOKEN":
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected endpoint %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	if err := setDependabotRepoSecret(context.Background(), client, sourceRepo, "TOKEN", value); err != nil {
		t.Fatal(err)
	}
	if err := deleteDependabotRepoSecret(context.Background(), client, sourceRepo, "TOKEN"); err != nil {
		t.Fatal(err)
	}
	if !wrote || !deleted {
		t.Fatalf("wrote=%v deleted=%v", wrote, deleted)
	}
}

func TestDependabotSecretRejectsInvalidPublicKey(t *check.T) {
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/public-key") {
			t.Error("invalid public key must not reach a write endpoint")
		}
		fmt.Fprint(w, `{"key_id":"id","key":"c2hvcnQ="}`)
	})
	err := setDependabotRepoSecret(context.Background(), client, sourceRepo, "TOKEN", "value")
	if err == nil || !strings.Contains(err.Error(), "public key length") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTokenRegistrationPreflightsAllNames(t *check.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing=%v", existing), func(t *check.T) {
			client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/repos/owner/repo/dependabot/secrets" {
					t.Error("preflight failure must not create or delete secrets")
				}
				if existing {
					fmt.Fprint(w, `{"secrets":[{"name":"token"}]}`)
				} else {
					fmt.Fprint(w, `{"secrets":[]}`)
				}
			})
			names := map[string]string{"a.example": "TOKEN", "b.example": "TOKEN"}
			err := registerTokenSecrets(context.Background(), client, sourceRepo, names, map[string]string{"a.example": "one", "b.example": "two"})
			if err == nil {
				t.Fatal("expected a collision error")
			}
		})
	}
}

func TestTokenRegistrationRollsBackAfterCancellation(t *check.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	public, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var created, deleted []string
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/public-key"):
			_ = json.NewEncoder(w).Encode(github.PublicKey{KeyID: github.Ptr("id"), Key: github.Ptr(base64.StdEncoding.EncodeToString(public[:]))})
		case r.Method == "GET":
			fmt.Fprint(w, `{"secrets":[]}`)
		case r.Method == "PUT":
			name := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			created = append(created, name)
			if name == "SECOND" {
				cancel()
				http.Error(w, `{"message":"interrupted"}`, http.StatusInternalServerError)
			} else {
				w.WriteHeader(http.StatusCreated)
			}
		case r.Method == "DELETE":
			deleted = append(deleted, r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:])
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	})
	err = registerTokenSecrets(ctx, client, sourceRepo,
		map[string]string{"a.example": "FIRST", "b.example": "SECOND"},
		map[string]string{"a.example": "one", "b.example": "two"})
	if err == nil || ctx.Err() == nil {
		t.Fatalf("expected cancellation failure, got %v", err)
	}
	want := []string{"FIRST", "SECOND"}
	if !reflect.DeepEqual(created, want) || !reflect.DeepEqual(deleted, want) {
		t.Fatalf("created %v deleted %v, want %v", created, deleted, want)
	}
}

func TestDependabotAPIErrorPreserved(t *check.T) {
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Forbidden"}`, http.StatusForbidden)
	})
	_, err := listDependabotRepoSecrets(context.Background(), client, sourceRepo)
	var apiErr *github.ErrorResponse
	if !errors.As(err, &apiErr) || apiErr.Response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected original API error, got %v", err)
	}
}

func TestCollectSecretFilters(t *check.T) {
	for _, org := range []bool{false, true} {
		t.Run(fmt.Sprintf("org=%v", org), func(t *check.T) {
			scope := migrator.SecretScopeRepo
			path := "/repos/owner/repo/dependabot/secrets"
			if org {
				scope = migrator.SecretScopeOrg
				path = "/orgs/owner/dependabot/secrets"
			}
			client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != path {
					t.Errorf("incorrect secret scope: %s", r.URL.Path)
				}
				fmt.Fprint(w, `{"secrets":[{"name":"FIRST"},{"name":"SECOND"}]}`)
			})
			names, err := collectSecrets(context.Background(), client, sourceRepo, &CopyConfig{Scope: scope, ExcludeSecrets: []string{"FIRST"}})
			if err != nil || !slices.Equal(names, []string{"SECOND"}) {
				t.Fatalf("names=%v err=%v", names, err)
			}
			names, err = collectSecrets(context.Background(), nil, sourceRepo, &CopyConfig{Secrets: []string{"EXPLICIT"}, Scope: scope})
			if err != nil || !slices.Equal(names, []string{"EXPLICIT"}) {
				t.Fatalf("explicit names=%v err=%v", names, err)
			}
		})
	}
}
