// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package jamle

import (
	"os"
)

// defaultMaxDepth bounds nested syntax and recursive resolver expansion
// when [UnmarshalOptions.MaxDepth] is not set.
const defaultMaxDepth = 32

// AssignmentMode controls side effects of ${VAR:=word} expansions.
type AssignmentMode uint8

const (
	// AssignmentSet is the default. When the variable is unset or empty,
	// the expanded word is returned and assigned through the resolver's [Setter].
	AssignmentSet AssignmentMode = iota
	// AssignmentIgnore returns the expanded word without calling [Setter].
	// This preserves the fallback value while disabling the assignment side effect.
	AssignmentIgnore
)

// RequiredMode controls the behavior of ${VAR:?word} when the variable is unset or empty.
type RequiredMode uint8

const (
	// RequiredError is the default.
	// It evaluates word only when needed and returns an error if the variable is unset or empty.
	RequiredError RequiredMode = iota
	// RequiredIgnore suppresses the required-variable error
	// and treats the expression as a plain variable. The fallback word remains lazy.
	RequiredIgnore
)

// Resolver provides variable lookup for ${VAR} expansions through [Resolver.Lookup].
// A nil Resolver in [UnmarshalOptions] selects the process environment.
// Values returned by a resolver may contain expansion syntax and are evaluated recursively,
// subject to [UnmarshalOptions.MaxDepth] and cycle detection.
type Resolver interface {
	Lookup(name string) (string, bool)
}

// Setter optionally supports the assignment side effect of ${VAR:=word} through [Setter.Set].
// It is used only when [AssignmentSet] is active and the variable is unset or empty.
// The default process-environment resolver implements [Setter].
type Setter interface {
	Set(name, value string) error
}

// UnmarshalOptions controls variable expansion behavior for [Unmarshal] APIs.
// Its zero value uses the process environment, a maximum depth of 32,
// assignment side effects enabled, and required-variable errors enabled.
type UnmarshalOptions struct {
	// Resolver provides values for ${VAR} expansion through [Resolver.Lookup].
	// When nil, the process environment resolver is used.
	// A custom resolver can also expose [Setter] to support := assignments.
	Resolver Resolver `json:"resolver" yaml:"resolver" jsonschema:"-"`

	// IgnoreExpandPaths skips expansion for scalar nodes
	// whose dot-separated YAML key path matches one of these path patterns.
	// An asterisk matches one path segment; unmatched paths are expanded normally.
	IgnoreExpandPaths []string `json:"ignoreExpandPaths,omitempty" yaml:"ignoreExpandPaths,omitempty"`

	// MaxDepth limits both nested ${...} syntax and recursive expansion of values returned by [Resolver].
	// Values <= 0 use the default depth of 32.
	// Exceeding the limit returns [ErrExpansionDepth] and does not produce a partial scalar value.
	MaxDepth int `json:"maxDepth,omitempty" yaml:"maxDepth,omitempty" jsonschema:"default=32,minimum=1,maximum=1000,example=32"`

	// Strict rejects unknown mapping keys when the destination contains a struct.
	// Maps accept arbitrary keys, and values targeting interface{} remain open.
	Strict bool `json:"strict,omitempty" yaml:"strict,omitempty" jsonschema:"default=false"`

	// Assignment controls the side effect of ${VAR:=word}.
	// [AssignmentSet] is the zero-value default and calls [Setter] when the variable is unset or empty.
	// If [Setter] is unavailable, expansion returns [ErrAssignmentUnsupported].
	// [AssignmentIgnore] evaluates word without changing the resolver.
	Assignment AssignmentMode `json:"assignment,omitempty" yaml:"assignment,omitempty" jsonschema:"default=0"`

	// Required controls errors from ${VAR:?word}.
	// RequiredError is the zero-value default and returns an error for an unset or empty variable;
	// [RequiredIgnore] suppresses that error while keeping word lazy.
	Required RequiredMode `json:"required,omitempty" yaml:"required,omitempty" jsonschema:"default=0"`

	// ExpandKeys enables expansion of scalar mapping keys.
	// The zero value keeps keys literal while values continue to expand.
	ExpandKeys bool `json:"expandKeys,omitempty" yaml:"expandKeys,omitempty" jsonschema:"default=false"`
}

// ResolveFunc adapts a function to the [Resolver] interface.
type ResolveFunc func(name string) (string, bool)

// envResolver resolves and assigns variables via process environment.
type envResolver struct{}

// runtimeOptions stores normalized expansion options for one unmarshal call.
type runtimeOptions struct {
	// resolver supplies values for the current unmarshal operation.
	resolver Resolver
	// ignorePathRules contains compiled scalar paths excluded from expansion.
	ignorePathRules []pathRule
	// maxDepth bounds syntax nesting and recursive resolver expansion.
	maxDepth int
	// allowAssignment controls whether ${VAR:=word} may call Setter.
	allowAssignment bool
	// enforceRequired controls whether ${VAR:?word} reports missing values.
	enforceRequired bool
	// expandKeys controls whether scalar mapping keys are expanded.
	expandKeys bool
}

// Lookup resolves a variable name via the underlying function.
func (f ResolveFunc) Lookup(name string) (string, bool) {
	return f(name)
}

// Lookup resolves a variable from process environment.
func (envResolver) Lookup(name string) (string, bool) {
	return os.LookupEnv(name)
}

// Set assigns a variable in process environment.
func (envResolver) Set(name, value string) error {
	return os.Setenv(name, value)
}
