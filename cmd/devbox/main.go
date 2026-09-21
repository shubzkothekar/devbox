// Command devbox is the host-side CLI for creating and configuring DevBox
// projects.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "devbox:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: devbox create <container-name> | devbox plugin <command>")
	}
	return fmt.Errorf("unsupported command %q", args[0])
}
