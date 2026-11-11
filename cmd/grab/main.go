package main

import (
	"fmt"
	"io"
	"os"

	"grab/internal/interpreter"
	"grab/internal/parser"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) != 2 || arguments[0] != "run" {
		fmt.Fprintln(stderr, "usage: grab run <file.grab>")
		return 2
	}
	source, err := os.ReadFile(arguments[1])
	if err != nil {
		fmt.Fprintf(stderr, "grab: %v\n", err)
		return 1
	}
	program, err := parser.Parse(arguments[1], string(source))
	if err != nil {
		fmt.Fprintf(stderr, "grab: %v\n", err)
		return 1
	}
	if err := interpreter.Run(program, stdout); err != nil {
		fmt.Fprintf(stderr, "grab: %v\n", err)
		return 1
	}
	return 0
}
