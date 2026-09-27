package variable
package variable

import (
	"reflect"
	"testing"
)

func TestMatchSelectedReposForDestination(t *testing.T) {
	source := []string{"repo-a", "repo-b", "missing"}
	existing := map[string]struct{}{"repo-a": {}, "repo-c": {}}

	got := matchSelectedReposForDestination(source, existing)
	want := []string{"repo-a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("matchSelectedReposForDestination() = %v, want %v", got, want)
	}
}
