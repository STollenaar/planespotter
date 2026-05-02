package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/nint8835/planespotter/pkg/config"
	"github.com/nint8835/planespotter/pkg/monitor"
)

var fetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Run a one-off fetch.",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.Load()
		checkErr(err, "Failed to load config")

		monitorInst, err := monitor.New(cfg)
		checkErr(err, "Failed to create monitor")

		err = monitorInst.FetchAndCheck(context.Background())
		checkErr(err, "Failed to fetch")
	},
}

func init() {
	rootCmd.AddCommand(fetchCmd)
}
