package main

import (
	"fmt"
	"grab/internal/contracts"
	"io"
)

func generateClient(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "client" {
		fmt.Fprintln(stderr, "usage: grab generate client --api-contract file --out directory [--react]")
		return 2
	}
	path, out := "", ""
	react := false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--react":
			if react {
				return 2
			}
			react = true
		case "--api-contract", "--out":
			flag := args[i]
			if i+1 >= len(args) {
				return 2
			}
			i++
			if flag == "--out" {
				if out != "" {
					return 2
				}
				out = args[i]
			} else {
				if path != "" {
					return 2
				}
				path = args[i]
			}
		default:
			return 2
		}
	}
	if path == "" || out == "" {
		return 2
	}
	d, e := contracts.Load(path)
	if e == nil {
		e = d.Generate(out, react)
	}
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	fmt.Fprintln(stdout, "Generated typed client", d.Hash())
	return 0
}
