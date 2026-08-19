package main

import (
	"fmt"
	"io"
	"os"

	"github.com/abyssbugg/ant-router/internal/buildinfo"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(stderr, "ant-router: unknown command %q\n", "")
		return 1
	}

	cmd := args[0]

	switch cmd {
	case "version":
		info := buildinfo.Current()
		fmt.Fprintf(stdout, "ant-router %s (commit %s, built %s)\n", info.Version, info.Commit, info.Date)
		return 0
	default:
		fmt.Fprintf(stderr, "ant-router: unknown command %q\n", cmd)
		return 1
	}
}
