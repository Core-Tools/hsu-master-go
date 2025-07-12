package main

import (
	"context"
	"fmt"
	"os"
	"time"

	sprintfLogging "github.com/core-tools/hsu-core/pkg/logging/sprintf"

	coreControl "github.com/core-tools/hsu-core/pkg/control"
	coreDomain "github.com/core-tools/hsu-core/pkg/domain"
	coreLogging "github.com/core-tools/hsu-core/pkg/logging"

	masterControl "github.com/core-tools/hsu-master/pkg/control"
	masterLogging "github.com/core-tools/hsu-master/pkg/logging"

	flags "github.com/jessevdk/go-flags"
)

type flagOptions struct {
	ServerPath string `long:"server" description:"path to the server executable"`
	AttachPort int    `long:"port" description:"port to attach to the server"`
}

func logPrefix(module string) string {
	return fmt.Sprintf("module: %s-client , ", module)
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

	logger.Infof("opts: %+v", opts)

	if opts.ServerPath == "" && opts.AttachPort == 0 {
		fmt.Println("Server path or attach port is required")
		os.Exit(1)
	}

	logger.Infof("Starting...")

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

	coreConnectionOptions := coreControl.ConnectionOptions{
		ServerPath: opts.ServerPath,
		AttachPort: opts.AttachPort,
	}
	coreConnection, err := coreControl.NewConnection(coreConnectionOptions, coreLogger)
	if err != nil {
		logger.Errorf("Failed to create core connection: %v", err)
		return
	}

	coreClientGateway := coreControl.NewGRPCClientGateway(coreConnection.GRPC(), coreLogger)
	masterClientGateway := masterControl.NewGRPCClientGateway(coreConnection.GRPC(), masterLogger)

	ctx := context.Background()

	retryPingOptions := coreDomain.RetryPingOptions{
		RetryAttempts: 10,
		RetryInterval: 1 * time.Second,
	}
	err = coreDomain.RetryPing(ctx, coreClientGateway, retryPingOptions, coreLogger)
	if err != nil {
		logger.Errorf("Failed to ping Echo server: %v", err)
		return
	}

	status, err := masterClientGateway.Status(ctx)
	if err != nil {
		logger.Errorf("Failed to get status: %v", err)
		return
	}

	logger.Infof("Status: %+v", status)

	logger.Infof("Done")
}
