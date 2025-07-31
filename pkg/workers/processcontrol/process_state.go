package processcontrol

// ProcessState represents the current lifecycle state of the process control
type ProcessState string

const (
	ProcessStateIdle        ProcessState = "idle"        // No process, ready to start
	ProcessStateStarting    ProcessState = "starting"    // Process startup in progress
	ProcessStateRunning     ProcessState = "running"     // Process running normally
	ProcessStateStopping    ProcessState = "stopping"    // Graceful shutdown initiated
	ProcessStateTerminating ProcessState = "terminating" // Force termination in progress
)
