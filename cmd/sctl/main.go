package main

import (
	"os"
)

func iferrexit(err error) {
	if err == nil {
		return
	}
	os.Exit(1)
}

func main() {
	cmd, err := newRootCmd(os.Stdout, os.Args[1:])
	iferrexit(err)
	iferrexit(cmd.Execute())
}
