package main

import (
	"context"
	"fmt"
	"os"

	"github.com/dencoseca/laptop-setup/internal/app"
)

func main() {
	if err := app.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
