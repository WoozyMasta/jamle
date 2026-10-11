// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package jamle

import (
	"errors"
	"fmt"
	"strings"

	jyaml "github.com/woozymasta/jamle/yaml"
)

var (
	// ErrAssignmentUnsupported is returned when the resolver does not support [Setter.Set].
	ErrAssignmentUnsupported = errors.New("assignment operator := is not supported by resolver")
	// ErrInvalidExpansion is returned for malformed or unmatched expansion syntax.
	ErrInvalidExpansion = errors.New("invalid environment expansion syntax")
	// ErrExpansionCycle is returned when a resolver value references a variable that is already active.
	ErrExpansionCycle = errors.New("environment expansion cycle")
	// ErrExpansionDepth is returned when expansion exceeds [UnmarshalOptions.MaxDepth].
	ErrExpansionDepth = errors.New("environment expansion depth exceeded")
	// ErrInvalidMaxDepth is returned when [UnmarshalOptions.MaxDepth] exceeds the hard limit.
	ErrInvalidMaxDepth = errors.New("invalid maximum expansion depth")
	// ErrExpansionSyntax is kept as a compatibility alias for [ErrInvalidExpansion].
	ErrExpansionSyntax = ErrInvalidExpansion
	// ErrRequiredVariable is returned when a required variable is unset or empty.
	ErrRequiredVariable = errors.New("required")
	// ErrOutMustBePointerToSlice reports an invalid out parameter for [UnmarshalAll] and [UnmarshalAllWithOptions].
	ErrOutMustBePointerToSlice = errors.New("out must be a pointer to slice")
	// ErrMultipleDocuments reports more than one document in single-document input.
	ErrMultipleDocuments = jyaml.ErrMultipleDocuments
)

// expansionSourceError adds logical path and YAML source coordinates to an expansion failure.
type expansionSourceError struct {
	cause  error  // cause is the underlying expansion failure.
	path   string // path is the dot-separated logical configuration path.
	line   int    // line is the one-based YAML source line.
	column int    // column is the one-based YAML source column.
}

// Error formats an expansion failure with its logical source context.
func (e *expansionSourceError) Error() string {
	return fmt.Sprintf("%s:%d:%d: %v", e.path, e.line, e.column, e.cause)
}

// Unwrap returns the underlying expansion failure for [errors.Is] and [errors.As].
func (e *expansionSourceError) Unwrap() error {
	return e.cause
}

// newExpansionSourceError creates a source-aware wrapper for one expansion error.
func newExpansionSourceError(path []string, line, column int, cause error) error {
	logicalPath := strings.Join(path, ".")
	if logicalPath == "" {
		logicalPath = "<root>"
	}

	return &expansionSourceError{
		path:   logicalPath,
		line:   line,
		column: column,
		cause:  cause,
	}
}
