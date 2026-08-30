package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/sinmetalcraft/bizmac/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	err := cli.NewRootCmd().ExecuteContext(ctx)
	if err != nil && err.Error() != "" {
		fmt.Fprintln(os.Stderr, "Error:", err)
	}
	os.Exit(cli.ExitCode(err))
}
