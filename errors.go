// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package jamle

import "errors"

var (
	// ErrAssignmentUnsupported is returned when resolver does not support Set.
	ErrAssignmentUnsupported = errors.New("assignment operator := is not supported by resolver")

	// ErrOutMustBePointerToSlice reports invalid out parameter for UnmarshalAll* APIs.
	ErrOutMustBePointerToSlice = errors.New("out must be a pointer to slice")
)
