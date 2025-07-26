package main

import (
	"fmt"
	"os"

	sprintflogging "github.com/core-tools/hsu-core/pkg/logging/sprintf"

	corelogging "github.com/core-tools/hsu-core/pkg/logging"
	masterlogging "github.com/core-tools/hsu-master/pkg/logging"
	master "github.com/core-tools/hsu-master/pkg/master"

	flags "github.com/jessevdk/go-flags"
)

type flagOptions struct {
	Config      string `long:"config" short:"c" description:"Configuration file path (YAML)" required:"true"`
	EnableLog   bool   `long:"enable-log" description:"Enable log collection (uses defaults if no config)"`
	RunDuration int    `long:"run-duration" description:"Duration in seconds to run the master (debug feature)"`
}

func logPrefix(module string) string {
	return fmt.Sprintf("module: %s-server , ", module)
}

func main() {
	var opts flagOptions
	var argv []string = os.Args[1:]
	var parser = flags.NewParser(&opts, flags.HelpFlag)
	var err error
	_, err = parser.ParseArgs(argv)
	if err != nil {
		fmt.Printf("Command line flags parsing failed: %v", err)
		os.Exit(1)
	}

	logger := sprintflogging.NewStdSprintfLogger()

	// Create loggers
	coreLogger := corelogging.NewLogger(
		logPrefix("hsu-core"), corelogging.LogFuncs{
			Debugf: logger.Debugf,
			Infof:  logger.Infof,
			Warnf:  logger.Warnf,
			Errorf: logger.Errorf,
		})
	masterLogger := masterlogging.NewLogger(
		logPrefix("hsu-master"), masterlogging.LogFuncs{
			Debugf: logger.Debugf,
			Infof:  logger.Infof,
			Warnf:  logger.Warnf,
			Errorf: logger.Errorf,
		})

	err = master.Run(opts.RunDuration, opts.Config, opts.EnableLog, coreLogger, masterLogger)
	if err != nil {
		logger.Errorf("Failed to run: %v", err)
		os.Exit(1)
	}
}
