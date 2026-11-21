package main

import (
	"embed"
	"fmt"
	"os"
)

//go:embed all:frontend/dist
var frontendFS embed.FS

func main() {
	cliOpts, err := parseCLIArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "参数错误: %v\n\n", err)
		writeCLIHelp(os.Stderr)
		os.Exit(2)
	}
	switch cliOpts.Action {
	case cliActionHelp:
		writeCLIHelp(os.Stdout)
		os.Exit(0)
	case cliActionVersion:
		printVersion(os.Stdout)
		os.Exit(0)
	}

	runService(cliOpts)
}
