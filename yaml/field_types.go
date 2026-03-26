// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

package yaml

import "reflect"

// field represents a single struct field recognized by JSON rules.
type field struct {
	typ       reflect.Type
	equalFold func(s, t []byte) bool // bytes.EqualFold or equivalent
	name      string
	nameBytes []byte // []byte(name)
	index     []int
	tag       bool
	omitEmpty bool
	quoted    bool
}

// byName sorts field by name, breaking ties with depth,
// then breaking ties with "name came from json tag", then
// breaking ties with index sequence.
type byName []field

// byIndex sorts field by index sequence.
type byIndex []field

// fillField enriches field metadata used by name matching.
func fillField(f field) field {
	f.nameBytes = []byte(f.name)
	f.equalFold = foldFunc(f.nameBytes)
	return f
}

// Len returns number of fields in the byName slice.
func (x byName) Len() int { return len(x) }

// Swap swaps fields at i and j in the byName slice.
func (x byName) Swap(i, j int) { x[i], x[j] = x[j], x[i] }

// Less compares fields by name ordering rules.
func (x byName) Less(i, j int) bool {
	if x[i].name != x[j].name {
		return x[i].name < x[j].name
	}

	if len(x[i].index) != len(x[j].index) {
		return len(x[i].index) < len(x[j].index)
	}

	if x[i].tag != x[j].tag {
		return x[i].tag
	}

	return byIndex(x).Less(i, j)
}

// Len returns number of fields in the byIndex slice.
func (x byIndex) Len() int { return len(x) }

// Swap swaps fields at i and j in the byIndex slice.
func (x byIndex) Swap(i, j int) { x[i], x[j] = x[j], x[i] }

// Less compares fields by index path order.
func (x byIndex) Less(i, j int) bool {
	for k, xik := range x[i].index {
		if k >= len(x[j].index) {
			return false
		}

		if xik != x[j].index[k] {
			return xik < x[j].index[k]
		}
	}

	return len(x[i].index) < len(x[j].index)
}
