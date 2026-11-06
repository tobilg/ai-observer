package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/tobilg/ai-observer/internal/logger"
)

// Custom error types for better error handling and categorization

// ValidationError represents errors from invalid input
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("validation error on field '%s': %s", e.Field, e.Message)
	}
	return fmt.Sprintf("validation error: %s", e.Message)
}

// NewValidationError creates a new validation error
func NewValidationError(field, message string) *ValidationError {
	return &ValidationError{Field: field, Message: message}
}

// StorageError represents errors from database operations
type StorageError struct {
	Operation string
	Cause     error
}

func (e *StorageError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("storage error during %s: %v", e.Operation, e.Cause)
	}
	return fmt.Sprintf("storage error during %s", e.Operation)
}

func (e *StorageError) Unwrap() error {
	return e.Cause
}

// NewStorageError creates a new storage error
func NewStorageError(operation string, cause error) *StorageError {
	return &StorageError{Operation: operation, Cause: cause}
}

// NotFoundError represents errors when a resource is not found
type NotFoundError struct {
	Resource string
	ID       string
}

func (e *NotFoundError) Error() string {
	if e.ID != "" {
		return fmt.Sprintf("%s with ID '%s' not found", e.Resource, e.ID)
	}
	return fmt.Sprintf("%s not found", e.Resource)
}

// NewNotFoundError creates a new not found error
func NewNotFoundError(resource, id string) *NotFoundError {
	return &NotFoundError{Resource: resource, ID: id}
}

// TimeoutError represents timeout errors
type TimeoutError struct {
	Operation string
	Duration  string
}

func (e *TimeoutError) Error() string {
	if e.Duration != "" {
		return fmt.Sprintf("timeout during %s after %s", e.Operation, e.Duration)
	}
	return fmt.Sprintf("timeout during %s", e.Operation)
}

// NewTimeoutError creates a new timeout error
func NewTimeoutError(operation, duration string) *TimeoutError {
	return &TimeoutError{Operation: operation, Duration: duration}
}

// PayloadTooLargeError represents errors when request payload exceeds size limit
type PayloadTooLargeError struct {
	MaxSize    int64
	ActualSize int64
}

func (e *PayloadTooLargeError) Error() string {
	return fmt.Sprintf("payload too large: maximum size is %d bytes, got %d bytes", e.MaxSize, e.ActualSize)
}

// NewPayloadTooLargeError creates a new payload too large error
func NewPayloadTooLargeError(maxSize, actualSize int64) *PayloadTooLargeError {
	return &PayloadTooLargeError{MaxSize: maxSize, ActualSize: actualSize}
}

// IsValidationError checks if an error is a ValidationError
func IsValidationError(err error) bool {
	var ve *ValidationError
	return errors.As(err, &ve)
}

// IsStorageError checks if an error is a StorageError
func IsStorageError(err error) bool {
	var se *StorageError
	return errors.As(err, &se)
}

// IsNotFoundError checks if an error is a NotFoundError
func IsNotFoundError(err error) bool {
	var nfe *NotFoundError
	return errors.As(err, &nfe)
}

// IsTimeoutError checks if an error is a TimeoutError
func IsTimeoutError(err error) bool {
	var te *TimeoutError
	return errors.As(err, &te)
}

// IsPayloadTooLargeError checks if an error is a PayloadTooLargeError
func IsPayloadTooLargeError(err error) bool {
	var ptle *PayloadTooLargeError
	return errors.As(err, &ptle)
}

// HTTPStatusFromError returns the appropriate HTTP status code for an error
func HTTPStatusFromError(err error) int {
	switch {
	case IsValidationError(err):
		return http.StatusBadRequest
	case IsNotFoundError(err):
		return http.StatusNotFound
	case IsTimeoutError(err):
		return http.StatusGatewayTimeout
	case IsPayloadTooLargeError(err):
		return http.StatusRequestEntityTooLarge
	case IsStorageError(err):
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

// WriteErrorFromError writes an error response based on the error type.
// Server errors are logged and answered with a generic message.
func WriteErrorFromError(w http.ResponseWriter, err error) {
	statusCode := HTTPStatusFromError(err)
	if statusCode >= http.StatusInternalServerError {
		logger.Error("Request failed", "status", statusCode, "error", err)
		WriteError(w, statusCode, http.StatusText(statusCode))
		return
	}
	WriteError(w, statusCode, err.Error())
}

// WriteInternalError logs err and writes a 500 response carrying only message,
// so internal details (SQL, file paths) stay in the server log.
func WriteInternalError(w http.ResponseWriter, message string, err error) {
	logger.Error(message, "error", err)
	WriteError(w, http.StatusInternalServerError, message)
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

func WriteError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error:   http.StatusText(statusCode),
		Message: message,
	})
}

func WriteJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
