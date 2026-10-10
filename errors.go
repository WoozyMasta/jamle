// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package jamle

import "errors"

var (
	// ErrAssignmentUnsupported is returned when resolver does not support Set.
	ErrAssignmentUnsupported = errors.New("assignment operator := is not supported by resolver")
	// ErrExpansionCycle is returned when a resolver value references a variable that is already active.
	ErrExpansionCycle = errors.New("environment expansion cycle")
	// ErrExpansionDepth is returned when expansion exceeds MaxDepth.
	ErrExpansionDepth = errors.New("environment expansion depth exceeded")
	// ErrExpansionSyntax is returned for an unmatched or malformed ${...} expression.
	ErrExpansionSyntax = errors.New("invalid environment expansion syntax")
	// ErrOutMustBePointerToSlice reports invalid out parameter for UnmarshalAll* APIs.
	ErrOutMustBePointerToSlice = errors.New("out must be a pointer to slice")
)
