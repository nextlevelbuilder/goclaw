package main

import (
	_ "time/tzdata" // embed IANA timezone database for containers without tzdata

	"github.com/nextlevelbuilder/goclaw/cmd"
)

func main() {
	cmd.Execute()
}
