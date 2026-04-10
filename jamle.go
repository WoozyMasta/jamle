// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package jamle

import (
	"os"
	"strings"
)

// placeholders for masking braces in escaped variables.
const (
	maskStart        = "\x00"
	maskEnd          = "\x01"
	defaultMaxPasses = 10
)

// Resolver provides variable lookup for ${VAR} expansions.
type Resolver interface {
	Lookup(name string) (string, bool)
}

// Setter optionally supports ${VAR:=default} assignment behavior.
type Setter interface {
	Set(name, value string) error
}

// UnmarshalOptions controls variable expansion behavior for Unmarshal APIs.
type UnmarshalOptions struct {
	// Resolver provides values for `${VAR}` expansion.
	// When nil, process environment resolver is used.
	Resolver Resolver `json:"resolver" yaml:"resolver" jsonschema:"-"`

	// IgnoreExpandPaths skips expansion for scalar nodes whose YAML key path
	// matches one of these glob patterns (dot-separated, `*` for one segment).
	IgnoreExpandPaths []string `json:"ignoreExpandPaths,omitempty" yaml:"ignoreExpandPaths,omitempty"`

	// MaxPasses limits nested expansion passes.
	// When <= 0, default max pass count is used.
	MaxPasses int `json:"maxPasses,omitempty" yaml:"maxPasses,omitempty" jsonschema:"default=10,minimum=1,maximum=1000,example=20"`

	// DisableAssignment disables side effects of `${VAR:=default}`.
	// When true, `${VAR:=default}` behaves like `${VAR:-default}` and does not call Setter.
	DisableAssignment bool `json:"disableAssignment,omitempty" yaml:"disableAssignment,omitempty" jsonschema:"default=false,example=true"`

	// DisableRequiredErrors disables errors for `${VAR:?message}`.
	// When true, `${VAR:?message}` behaves like `${VAR}` and does not return an error.
	DisableRequiredErrors bool `json:"disableRequiredErrors,omitempty" yaml:"disableRequiredErrors,omitempty" jsonschema:"default=false,example=true"`
}

// ResolveFunc adapts a function to the Resolver interface.
type ResolveFunc func(name string) (string, bool)

// envResolver resolves and assigns variables via process environment.
type envResolver struct{}

// runtimeOptions stores normalized expansion options for one unmarshal call.
type runtimeOptions struct {
	resolver        Resolver
	ignorePathRules []pathRule
	maxPasses       int
	allowAssignment bool
	enforceRequired bool
}

// unmaskReplacer restores masked escaped variables back to ${...}.
var unmaskReplacer = strings.NewReplacer(maskStart, "${", maskEnd, "}")

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
