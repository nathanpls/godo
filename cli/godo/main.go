package main

import (
	"fmt"
	"os"
)

func main() {
	application, err := newApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, "godo:", err)
		os.Exit(1)
	}
	if err := application.run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "godo:", err)
		os.Exit(1)
	}
}
