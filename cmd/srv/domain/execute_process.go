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

		/*
			err := resetConsoleSignals(false)
			if err != nil {
				return nil, nil, NewInternalError("failed to reset console signals", err)
			}
		*/

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
			return nil, nil, NewProcessError("failed to start the process", err).WithContext("executable_path", execution.ExecutablePath)
		}

		/*
			err = resetConsoleSignals(true)
			if err != nil {
				return nil, nil, NewInternalError("failed to reset console signals", err)
			}
		*/

		return cmd, stdout, nil
	}
}
