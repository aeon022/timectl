package main

import (
	"github.com/aeon022/missionctl-core/secure"
	"github.com/aeon022/timectl/cmd"
)

func main() {
	secure.Init("timectl") // owner-only files from the first byte
	cmd.Execute()
}
