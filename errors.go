// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package jamle

import "errors"

var (
	// ErrAssignmentUnsupported is returned when the resolver does not support [Setter.Set].
	ErrAssignmentUnsupported = errors.New("assignment operator := is not supported by resolver")
	// ErrExpansionCycle is returned when a resolver value references a variable that is already active.
	ErrExpansionCycle = errors.New("environment expansion cycle")
	// ErrExpansionDepth is returned when expansion exceeds [UnmarshalOptions.MaxDepth].
	ErrExpansionDepth = errors.New("environment expansion depth exceeded")
	// ErrExpansionSyntax is returned for an unmatched or malformed ${...} expression.
	ErrExpansionSyntax = errors.New("invalid environment expansion syntax")
	// ErrOutMustBePointerToSlice reports an invalid out parameter for [UnmarshalAll] and [UnmarshalAllWithOptions].
	ErrOutMustBePointerToSlice = errors.New("out must be a pointer to slice")
)
