// Command pigeon is the entry point for the PigeonCLI email client.
package main

import (
	"fmt"
	"os"

	"github.com/tomowang/pigeoncli/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
