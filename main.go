/*
Copyright © 2025 srz_zumix
*/
package main

import (
	"embed"

	"github.com/srz-zumix/gh-secret-kit/cmd"
)

//go:embed skills
var skillsFS embed.FS

func main() {
	cmd.RegisterSkillsCmd(skillsFS)
	cmd.Execute()
}
