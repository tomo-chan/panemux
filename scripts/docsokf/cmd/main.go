package main

import (
	"os"

	"panemux/scripts/docsokf"
)

//coverage:exempt CLI process boundary; Run is tested directly without terminating the test process
func main() { os.Exit(docsokf.Run(os.Args[1:], os.Stdout, os.Stderr)) }
