// Command copse manages task-scoped git worktrees: create them under a
// name, carry env files in, and prune them when their branch merges.
package main

import (
	"os"

	"github.com/JaydenCJ/copse/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
