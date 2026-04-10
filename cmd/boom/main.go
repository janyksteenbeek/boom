package main

import (
	"fmt"
	"os"

	"github.com/janyksteenbeek/boom/internal/app"
)

func main() {
	a, err := app.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "boom: %v\n", err)
		os.Exit(1)
	}
	a.Run()
}
