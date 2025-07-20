package domain

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/core-tools/hsu-master/pkg/logging"
)

// ensureExecutable checks if a file is executable and makes it executable if it's not
func ensureExecutable(path string) error {
	// Check if file exists
	info, err := os.Stat(path)
	if err != nil {
		return NewIOError("file does not exist", err).WithContext("path", path)
	}

	// On Windows, files with .exe, .bat, .cmd extensions are inherently executable
	// Also, system files in Windows system directories are already executable
	if runtime.GOOS == "windows" {
		ext := filepath.Ext(path)
		if ext == ".exe" || ext == ".bat" || ext == ".cmd" {
			return nil // Already executable
		}
		// For Windows system directories, assume files are executable
		if filepath.Dir(path) == "C:\\Windows\\System32" || filepath.Dir(path) == "C:\\Windows\\System32\\" {
			return nil // System files are already executable
		}
	}

	// Check if file is already executable
	mode := info.Mode()
	if mode&0111 != 0 { // Check if any execute bit is set
		return nil // Already executable
	}

	// Try to make it executable (only on Unix systems or non-system files)
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, mode|0111); err != nil {
			return NewPermissionError("failed to make file executable", err).WithContext("path", path)
		}
	}

	return nil
}

/*
	type Controller interface {
		Stop()
	}

	type controller struct {
		monitor *monitor
	}

	func (p *controller) Stop() {
		p.monitor.stop()
	}

	type ControllerLogConfig struct {
		Module string
		Funcs  logging.LogFuncs
	}

	func newLogger(module, subModule string, funcs logging.LogFuncs) logging.Logger {
		prefix := ""
		if module != "" {
			prefix = "module: " + module + "." + subModule + " , "
		}
		return logging.NewLogger(prefix, funcs)
	}

	func NewController(path string, args []string, retryPeriod time.Duration, logConfig ControllerLogConfig) (Controller, error) {
		ctx, cancel := context.WithCancel(context.Background())

		command := &command{
			path:   path,
			args:   args,
			logger: newLogger(logConfig.Module, "command", logConfig.Funcs),
		}

		output := &output{
			logger: newLogger(logConfig.Module, "output", logConfig.Funcs),
		}

		if retryPeriod == 0 {
			retryPeriod = 10 * time.Second // Default retry period
		}

		monitor := &monitor{
			command:     command,
			output:      output,
			ctx:         ctx,
			cancel:      cancel,
			logger:      newLogger(logConfig.Module, "monitor", logConfig.Funcs),
			retryPeriod: retryPeriod,
		}

		monitor.start()

		return &controller{
			monitor: monitor,
		}, nil
	}

	type monitor struct {
		command     *command
		output      *output
		logger      logging.Logger
		ctx         context.Context
		cancel      context.CancelFunc
		wg          sync.WaitGroup
		retryPeriod time.Duration
	}

	func (l *monitor) loop() {
		defer l.wg.Done()

		l.logger.Debugf("Starting process monitor loop")
		defer func() {
			l.logger.Debugf("Process monitor loop stopped")
		}()

		retryPeriod := 10 * time.Second
		if l.retryPeriod > 0 {
			retryPeriod = l.retryPeriod
		}

		for loopCount := 1; ; loopCount++ {
			// Reduce log verbosity for frequent restarts - log first 5 attempts, then every 10th
			if loopCount <= 5 || loopCount%10 == 0 {
				l.logger.Debugf("Starting process (iteration %d): %s %v", loopCount, l.command.path, l.command.args)
			}
			cmd, stdout, err := l.command.start(l.ctx)
			if err != nil {
				l.logger.Errorf("Failed to start process (iteration %d): %v, retrying in %v seconds", loopCount, err, retryPeriod)

				select {
				case <-l.ctx.Done():
					l.logger.Debugf("Cancelled while waiting for process to start")
					return
				case <-time.After(retryPeriod):
					continue
				}
			}

			l.logger.Debugf("Process started, PID: %d, setting up output reader", cmd.Process.Pid)
			var outputWg sync.WaitGroup
			outputWg.Add(1)
			go func() {
				defer func() {
					outputWg.Done()
					l.logger.Debugf("Output reader finished")
				}()
				l.logger.Debugf("Output reader starting")
				l.output.read(stdout)
			}()

			l.logger.Debugf("Waiting for process (PID: %d) to exit", cmd.Process.Pid)
			err = cmd.Wait()
			l.logger.Debugf("Process exited (PID: %d), waiting for output reader", cmd.Process.Pid)
			// Wait for reader to finish after process exits
			outputWg.Wait()

			if err != nil {
				l.logger.Errorf("Process exited with error: %v", err)
			} else {
				l.logger.Debugf("Process exited successfully")
			}

			select {
			case <-l.ctx.Done():
				l.logger.Debugf("Cancelled while waiting for process to exit")
				return
			default:
				l.logger.Debugf("Process exited, will restart")
			}
		}
	}

	func (l *monitor) start() {
		l.wg.Add(1)
		go l.loop()
	}

	func (l *monitor) stop() {
		l.logger.Debugf("Stopping monitor...")
		l.cancel()
		l.logger.Debugf("Waiting for monitor to stop...")
		l.wg.Wait()
		l.logger.Debugf("Monitor stopped")
	}

	type output struct {
		logger logging.Logger
	}

	func (r *output) read(stdout io.ReadCloser) {
		defer stdout.Close()
		lineCount := 0
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lineCount++
			// Note: Individual line output logging removed to prevent log flooding
			// Process output is still captured and can be accessed via other means
		}
		if err := scanner.Err(); err != nil {
			r.logger.Errorf("stdout.Read failed: %v", err)
		}
		r.logger.Debugf("Process output captured: %d lines", lineCount)
	}
*/

type ExecutionConfig struct {
	ExecutablePath   string        `yaml:"executable_path"`
	Args             []string      `yaml:"args,omitempty"`
	Environment      []string      `yaml:"environment,omitempty"`
	WorkingDirectory string        `yaml:"working_directory,omitempty"`
	WaitDelay        time.Duration `yaml:"wait_delay,omitempty"`

	// PID file configuration (optional)
	ProcessFileConfig *ProcessFileConfig `yaml:"process_file,omitempty"`
}

type StdExecuteCmd func(ctx context.Context) (*exec.Cmd, io.ReadCloser, error)

func NewStdExecuteCmd(execution ExecutionConfig, logger logging.Logger) StdExecuteCmd {
	return func(ctx context.Context) (*exec.Cmd, io.ReadCloser, error) {
		// Validate context
		if ctx == nil {
			return nil, nil, NewValidationError("context cannot be nil", nil)
		}

		// Validate execution config
		if err := ValidateExecutionConfig(execution); err != nil {
			return nil, nil, NewValidationError("invalid execution configuration", err)
		}

		logger.Infof("Executing process, execution config: %+v", execution)

		// Check if the process is executable, and make it executable if it's not
		if err := ensureExecutable(execution.ExecutablePath); err != nil {
			return nil, nil, NewPermissionError("failed to ensure process is executable", err).WithContext("executable_path", execution.ExecutablePath)
		}

		workDir := execution.WorkingDirectory
		if workDir == "" {
			absPath, err := filepath.Abs(execution.ExecutablePath)
			if err != nil {
				return nil, nil, NewIOError("failed to get absolute path", err).WithContext("executable_path", execution.ExecutablePath)
			}
			workDir = filepath.Dir(absPath)
		}

		logger.Debugf("Executing process: '%s', args: %v, working directory: '%s'",
			execution.ExecutablePath, execution.Args, workDir)

		env := os.Environ()
		for _, e := range execution.Environment {
			env = append(env, e)
		}

		cmd := exec.CommandContext(ctx, execution.ExecutablePath, execution.Args...)
		cmd.Dir = workDir
		cmd.Env = env

		// Platform-specific setup is handled in process_windows.go or process_unix.go
		setupProcessAttributes(cmd)

		// wait after sending the interrupt signal, before sending the kill signal
		cmd.WaitDelay = execution.WaitDelay

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, nil, NewProcessError("failed to create stdout pipe", err).WithContext("executable_path", execution.ExecutablePath)
		}
		cmd.Stderr = cmd.Stdout

		logger.Infof("Starting process, cmd: %+v", cmd)

		err = cmd.Start()
		if err != nil {
			return nil, nil, NewProcessError("failed to start the app", err).WithContext("executable_path", execution.ExecutablePath)
		}

		return cmd, stdout, nil
	}
}
