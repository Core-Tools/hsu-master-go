package main

import (
	"fmt"
	"os"

	sprintfLogging "github.com/core-tools/hsu-core/pkg/logging/sprintf"

	coreLogging "github.com/core-tools/hsu-core/pkg/logging"
	masterLogging "github.com/core-tools/hsu-master/pkg/logging"
	master "github.com/core-tools/hsu-master/pkg/master"

	flags "github.com/jessevdk/go-flags"
)

type flagOptions struct {
	Config    string `long:"config" short:"c" description:"Configuration file path (YAML)" required:"true"`
	EnableLog bool   `long:"enable-log" description:"Enable log collection (uses defaults if no config)"`
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

	logger := sprintfLogging.NewStdSprintfLogger()

	// Create loggers
	coreLogger := coreLogging.NewLogger(
		logPrefix("hsu-core"), coreLogging.LogFuncs{
			Debugf: logger.Debugf,
			Infof:  logger.Infof,
			Warnf:  logger.Warnf,
			Errorf: logger.Errorf,
		})
	masterLogger := masterLogging.NewLogger(
		logPrefix("hsu-master"), masterLogging.LogFuncs{
			Debugf: logger.Debugf,
			Infof:  logger.Infof,
			Warnf:  logger.Warnf,
			Errorf: logger.Errorf,
		})

	// Run with configuration file
	logger.Infof("Starting HSU Master with configuration file: %s", opts.Config)

	if opts.EnableLog {
		logger.Infof("Log collection is ENABLED - worker logs will be collected!")
		// TODO: Use enhanced runner when ready
		err = master.RunWithConfigAndLogCollection(opts.Config, coreLogger, masterLogger)
	} else {
		logger.Infof("Log collection is DISABLED - using standard runner")
		err = master.RunWithConfig(opts.Config, coreLogger, masterLogger)
	}

	if err != nil {
		logger.Errorf("Failed to run with configuration: %v", err)
		os.Exit(1)
	}
}
