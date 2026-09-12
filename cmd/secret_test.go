package cmd

import "testing"

func TestSecretDependabotCopyRegistered(t *testing.T) {
	root := NewSecretCmd()
	command, args, err := root.Find([]string{"dependabot", "copy", "owner/destination"})
	if err != nil {
		t.Fatal(err)
	}
	if got := command.CommandPath(); got != "secret dependabot copy" {
		t.Fatalf("command path = %q, want secret dependabot copy", got)
	}
	if len(args) != 1 || args[0] != "owner/destination" {
		t.Fatalf("destination arguments = %v, want [owner/destination]", args)
	}
}
