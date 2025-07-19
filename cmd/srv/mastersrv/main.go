package main

import (
	"context"
	"fmt"
	"os"
	"time"

	sprintfLogging "github.com/core-tools/hsu-core/pkg/logging/sprintf"

	coreLogging "github.com/core-tools/hsu-core/pkg/logging"
	domain "github.com/core-tools/hsu-master/cmd/srv/domain"
	masterLogging "github.com/core-tools/hsu-master/pkg/logging"

	flags "github.com/jessevdk/go-flags"
)

type flagOptions struct {
	Port       int    `long:"port" description:"port to listen on"`
	Process    string `long:"process" description:"process to start"`
	Integrated bool   `long:"integrated" description:"use integrated mode"`
	Managed    bool   `long:"managed" description:"use managed mode"`
	Unmanaged  bool   `long:"unmanaged" description:"use unmanaged mode"`
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

	logger.Infof("opts: %+v", opts)

	if opts.Port == 0 {
		fmt.Println("Port is required")
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

	// Create and start the master server
	masterOptions := domain.MasterOptions{
		Port: opts.Port,
	}
	master, err := domain.NewMaster(masterOptions, coreLogger, masterLogger)
	if err != nil {
		logger.Errorf("Failed to create Master: %v", err)
		os.Exit(1)
	}

	workers := []domain.Worker{}
	if opts.Integrated {
		unit := &domain.IntegratedUnit{
			Metadata: domain.UnitMetadata{
				Name: "test-integrated",
			},
			Control: domain.ManagedProcessControlConfig{
				Execution: domain.ExecutionConfig{
					ExecutablePath: opts.Process,
					WaitDelay:      10 * time.Second,
				},
				Restart: domain.RestartConfig{
					Policy:      domain.RestartAlways,
					MaxRetries:  3,
					RetryDelay:  10 * time.Second,
					BackoffRate: 1.5,
				},
			},
			HealthCheckRunOptions: domain.HealthCheckRunOptions{
				Interval: 10 * time.Second,
				Timeout:  10 * time.Second,
			},
		}
		worker := domain.NewIntegratedWorker("test-integrated", unit, masterLogger)
		workers = append(workers, worker)
	}
	if opts.Managed {
		unit := &domain.ManagedUnit{
			Metadata: domain.UnitMetadata{
				Name: "test-managed",
			},
			Control: domain.ManagedProcessControlConfig{
				Execution: domain.ExecutionConfig{
					ExecutablePath: opts.Process,
					WaitDelay:      10 * time.Second,
				},
				Restart: domain.RestartConfig{
					Policy:      domain.RestartAlways,
					MaxRetries:  3,
					RetryDelay:  10 * time.Second,
					BackoffRate: 1.5,
				},
			},
			HealthCheck: domain.HealthCheckConfig{
				Type: domain.HealthCheckTypeHTTP,
				HTTP: domain.HTTPHealthCheckConfig{
					URL: "http://localhost:8080/health",
				},
				RunOptions: domain.HealthCheckRunOptions{
					Interval: 10 * time.Second,
					Timeout:  10 * time.Second,
				},
			},
		}
		worker := domain.NewManagedWorker("test-managed", unit, masterLogger)
		workers = append(workers, worker)
	}
	if opts.Unmanaged {
		unit := &domain.UnmanagedUnit{
			Metadata: domain.UnitMetadata{
				Name: "test-unmanaged",
			},
		}
		worker := domain.NewUnmanagedWorker("test-unmanaged", unit, masterLogger)
		workers = append(workers, worker)
	}

	ctx := context.Background()

	for _, worker := range workers {
		// Add worker (registration only)
		err = master.AddWorker(worker)
		if err != nil {
			logger.Errorf("Failed to add worker: %v", err)
			os.Exit(1)
		}

		// Start worker (lifecycle management)
		err = master.StartWorker(ctx, worker.ID())
		if err != nil {
			logger.Errorf("Failed to start worker: %v", err)
			os.Exit(1)
		}
	}

	master.Run(ctx)
}
