package main

import (
	"os"

	"herdlite/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
