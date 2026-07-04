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
	"github.com/nint8835/planespotter/pkg/web"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Start the monitor.",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.Load()
		checkErr(err, "Failed to load config")

		monitorInst, err := monitor.New(cfg)
		checkErr(err, "Failed to create monitor")

		apiServer, err := web.NewServer(cfg)
		checkErr(err, "Failed to create HTTP server")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errc := make(chan runResult, 2)
		go func() {
			errc <- runResult{name: "monitor", err: monitorInst.Run(ctx)}
		}()
		go func() {
			errc <- runResult{name: "http_server", err: apiServer.Run(ctx)}
		}()

		quit := make(chan os.Signal, 1)
		signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(quit)

		const serviceCount = 2
		remaining := serviceCount
		var result runResult
		select {
		case <-quit:
		case result = <-errc:
			remaining--
		}

		cancel()

		if result.err != nil && !errors.Is(result.err, context.Canceled) {
			slog.Error("Error running service", "service", result.name, "err", result.err)
			os.Exit(1)
		}
		if result := waitForRunResults(errc, remaining); result.err != nil {
			slog.Error("Error running service", "service", result.name, "err", result.err)
			os.Exit(1)
		}
	},
}

type runResult struct {
	name string
	err  error
}

func waitForRunResults(errc <-chan runResult, count int) runResult {
	for range count {
		result := <-errc
		if result.err != nil && !errors.Is(result.err, context.Canceled) {
			return result
		}
	}

	return runResult{}
}

func init() {
	rootCmd.AddCommand(runCmd)
}
