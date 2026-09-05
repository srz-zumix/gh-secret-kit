package migrator

import "testing"

func TestParseRenameMappings(t *testing.T) {
	got, err := ParseRenameMappings([]string{"OLD=NEW", "FOO_BAR=BAZ"})
	if err != nil {
		t.Fatalf("ParseRenameMappings returned error: %v", err)
	}
	if got["OLD"] != "NEW" || got["FOO_BAR"] != "BAZ" {
		t.Errorf("unexpected rename map: %v", got)
	}
}

func TestParseRenameMappingsRejectsInvalid(t *testing.T) {
	cases := []string{
		"OLD",               // missing '='
		"=NEW",              // empty source
		"OLD=",              // empty destination
		"OLD=NEW; rm -rf /", // shell metacharacters in destination
		"BAD NAME=NEW",      // space in source
		"1BAD=NEW",          // leading digit in source
	}
	for _, c := range cases {
		if _, err := ParseRenameMappings([]string{c}); err == nil {
			t.Errorf("expected an error for rename mapping %q", c)
		}
	}
}

func TestValidateSecretName(t *testing.T) {
	valid := []string{"FOO", "foo_bar", "_x", "A1"}
	for _, name := range valid {
		if err := ValidateSecretName(name); err != nil {
			t.Errorf("expected %q to be valid, got %v", name, err)
		}
	}
	invalid := []string{"", "1FOO", "FOO-BAR", "FOO BAR", "FOO;rm", "FOO$X"}
	for _, name := range invalid {
		if err := ValidateSecretName(name); err == nil {
			t.Errorf("expected %q to be rejected", name)
		}
	}
}
