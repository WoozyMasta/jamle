// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package jamle_test

import (
	"fmt"

	"github.com/woozymasta/jamle"
)

// exampleResolver provides deterministic values for package examples.
type exampleResolver map[string]string

// Lookup resolves an example variable without reading process environment.
func (r exampleResolver) Lookup(name string) (string, bool) {
	value, ok := r[name]
	return value, ok
}

func ExampleUnmarshalWithOptions() {
	type Config struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}

	var cfg Config
	err := jamle.UnmarshalWithOptions(
		[]byte("host: ${HOST:-localhost}\nport: ${PORT:-8080}\n"),
		&cfg,
		jamle.UnmarshalOptions{
			Resolver: exampleResolver{
				"HOST": "svc.local",
				"PORT": "9000",
			},
		},
	)
	if err != nil {
		panic(err)
	}

	fmt.Printf("%s:%d\n", cfg.Host, cfg.Port)
	// Output: svc.local:9000
}

func ExampleUnmarshalWithOptions_noexpand() {
	type Config struct {
		Script string `json:"script" jamle:"noexpand"`
		Args   string `json:"args"`
	}

	var cfg Config
	err := jamle.UnmarshalWithOptions(
		[]byte("script: \"echo ${HOME}\"\nargs: \"${ARGS:---verbose}\"\n"),
		&cfg,
		jamle.UnmarshalOptions{Resolver: exampleResolver{}},
	)
	if err != nil {
		panic(err)
	}

	fmt.Printf("%q\n%q\n", cfg.Script, cfg.Args)
	// Output:
	// "echo ${HOME}"
	// "--verbose"
}

func ExampleUnmarshalWithOptions_ignoreExpandPaths() {
	var cfg struct {
		Command string `json:"command"`
		Value   string `json:"value"`
	}
	err := jamle.UnmarshalWithOptions(
		[]byte("command: \"echo ${HOME}\"\nvalue: \"${VALUE:-ok}\"\n"),
		&cfg,
		jamle.UnmarshalOptions{
			Resolver:          exampleResolver{},
			IgnoreExpandPaths: []string{"command"},
		},
	)
	if err != nil {
		panic(err)
	}

	fmt.Printf("%q\n%q\n", cfg.Command, cfg.Value)
	// Output:
	// "echo ${HOME}"
	// "ok"
}

func ExampleUnmarshalWithOptions_expandKeys() {
	var cfg map[string]string
	err := jamle.UnmarshalWithOptions(
		[]byte("${KEY}: \"${VALUE:-value}\"\n"),
		&cfg,
		jamle.UnmarshalOptions{
			Resolver:   exampleResolver{"KEY": "expanded"},
			ExpandKeys: true,
		},
	)
	if err != nil {
		panic(err)
	}

	fmt.Println(cfg["expanded"])
	// Output: value
}

func ExampleUnmarshalAll() {
	type Config struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}

	var all []Config
	err := jamle.UnmarshalAll([]byte("---\nhost: a.local\nport: 8080\n---\nhost: b.local\nport: 9090\n"), &all)
	if err != nil {
		panic(err)
	}

	for _, cfg := range all {
		fmt.Printf("%s:%d\n", cfg.Host, cfg.Port)
	}
	// Output:
	// a.local:8080
	// b.local:9090
}
