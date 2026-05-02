package cmd

import "github.com/spf13/cobra"

var rootCmd = &cobra.Command{
	Use:   "planespotter",
	Short: "Monitor aircraft seen by a local tar1090 instance.",
}

func Execute() {
	err := rootCmd.Execute()
	checkErr(err, "Failed to execute")
}
