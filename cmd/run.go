package cmd

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/nint8835/planespotter/pkg/config"
	"github.com/nint8835/planespotter/pkg/monitor"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Start the monitor.",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.Load()
		checkErr(err, "Failed to load config")

		monitorInst, err := monitor.New(cfg)
		checkErr(err, "Failed to create monitor")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errc := make(chan error, 1)
		go func() {
			errc <- monitorInst.Run(ctx)
		}()

		quit := make(chan os.Signal, 1)
		signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(quit)

		select {
		case <-quit:
			cancel()
			if err := <-errc; err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("Error running monitor", "err", err)
				os.Exit(1)
			}
		case err := <-errc:
			checkErr(err, "Error running monitor")
		}
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
}
