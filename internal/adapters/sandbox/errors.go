package sandbox

import "errors"

var (
	ErrSetupFailed      = errors.New("sandbox setup failed")
	ErrPolicyDenied     = errors.New("sandbox policy denied")
	ErrExecutionFailed  = errors.New("sandbox execution failed")
	ErrExecutionTimeout = errors.New("sandbox execution timed out")
)
