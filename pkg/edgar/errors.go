package edgar

import "fmt"

type ErrorCode string

const (
	ErrorValidationRequired ErrorCode = "VALIDATION_ERROR"
	ErrorDocsRequired       ErrorCode = "DOCS_REQUIRED"
	ErrorIdentityRequired   ErrorCode = "IDENTITY_REQUIRED"
	ErrorRateLimited        ErrorCode = "RATE_LIMITED"
	ErrorNotFound           ErrorCode = "NOT_FOUND"
	ErrorNetwork            ErrorCode = "NETWORK_ERROR"
	ErrorParse              ErrorCode = "PARSE_ERROR"
	ErrorInternal           ErrorCode = "INTERNAL_ERROR"
)

var exitCodeByError = map[ErrorCode]int{
	ErrorValidationRequired: 2,
	ErrorDocsRequired:       2,
	ErrorIdentityRequired:   3,
	ErrorRateLimited:        4,
	ErrorNotFound:           5,
	ErrorNetwork:            6,
	ErrorParse:              7,
	ErrorInternal:           10,
}

type CLIError struct {
	Code      ErrorCode
	Message   string
	Retriable bool
}

func (e *CLIError) Error() string {
	return e.Message
}

func (e *CLIError) ExitCode() int {
	if code, ok := exitCodeByError[e.Code]; ok {
		return code
	}
	return 10
}

func NewCLIError(code ErrorCode, message string) *CLIError {
	return &CLIError{Code: code, Message: message}
}

func NewRetriableCLIError(code ErrorCode, message string) *CLIError {
	return &CLIError{Code: code, Message: message, Retriable: true}
}

func validationError(format string, args ...any) *CLIError {
	return NewCLIError(ErrorValidationRequired, fmt.Sprintf(format, args...))
}

func toCLIError(err error) *CLIError {
	if err == nil {
		return nil
	}
	if cliErr, ok := err.(*CLIError); ok {
		return cliErr
	}
	return NewCLIError(ErrorInternal, err.Error())
}
