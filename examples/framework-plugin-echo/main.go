package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/heiyeluren/mz-gui/framework/go/adapters/plugin/sdk"
)

func main() {
	if err := run(context.Background(), os.Stdin, os.Stdout, os.Stderr); err != nil {
		os.Exit(1)
	}
}

func run(ctx context.Context, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	if err := sdk.ServeJSONLines(ctx, stdin, stdout, sdk.StaticHandler{Descriptor: pluginDescriptor()}); err != nil {
		fmt.Fprintln(stderr, err)
		return err
	}
	return nil
}

func pluginDescriptor() sdk.Descriptor {
	return sdk.Descriptor{
		PluginID:      "example.echo",
		ProtocolMajor: 1,
		ProtocolMinor: 0,
		BuildID:       "example-dev",
		Capabilities:  []string{"adapter.browser"},
	}
}
