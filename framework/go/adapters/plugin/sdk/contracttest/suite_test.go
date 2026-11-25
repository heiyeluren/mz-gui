package contracttest

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/adapters/plugin"
	"github.com/heiyeluren/mz-gui/framework/go/adapters/plugin/sdk"
)

func TestMain(m *testing.M) {
	if os.Getenv("MZGUI_PLUGIN_CONTRACT_CHILD") == "1" {
		err := sdk.ServeJSONLines(context.Background(), os.Stdin, os.Stdout, sdk.StaticHandler{
			Descriptor: sdk.Descriptor{
				PluginID:      "example.echo",
				ProtocolMajor: 1,
				ProtocolMinor: 0,
				BuildID:       "test-child",
				Capabilities:  []string{"adapter.browser"},
			},
		})
		if err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestRunBasicContractSuiteAgainstSubprocess(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	RunBasic(t, SuiteOptions{
		Command: Command{
			Path: executable,
			Env:  append(os.Environ(), "MZGUI_PLUGIN_CONTRACT_CHILD=1"),
		},
		Manifest: plugin.Manifest{
			PluginID:              "example.echo",
			RequestedCapabilities: []string{"adapter.browser"},
			Process: plugin.Process{
				ProtocolMajor: 1,
				ProtocolMinor: 0,
				BuildID:       "test-child",
			},
		},
		Timeout: 2 * time.Second,
	})
}
