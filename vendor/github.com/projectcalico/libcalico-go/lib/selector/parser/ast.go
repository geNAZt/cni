// Copyright (c) 2016-2017 Tigera, Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package parser

import (
	_ "crypto/sha256" // register hash func
	"strings"

	"github.com/projectcalico/libcalico-go/lib/hash"
)

// Labels defines the interface of labels that can be used by selector
type Labels interface {
	// Get returns value and presence of the given labelName
	Get(labelName string) (value string, present bool)
}

// MapAsLabels allows you use map as labels
type MapAsLabels map[string]string

// Get returns the value and presence of the given labelName key in the MapAsLabels
func (l MapAsLabels) Get(labelName string) (value string, present bool) {
	value, present = l[labelName]
	return
}

// Selector represents a label selector.
type Selector interface {
	// Evaluate evaluates the selector against the given labels expressed as a concrete map.
	Evaluate(labels map[string]string) bool
	// EvaluateLabels evaluates the selector against the given labels expressed as an interface.
	// This allows for labels that are calculated on the fly.
	EvaluateLabels(labels Labels) bool
	// String returns a string that represents this selector.
	String() string
	// UniqueID returns the unique ID that represents this selector.
	UniqueID() string
}

type selectorRoot struct {
	root         node
	cachedString *string
	cachedHash   *string
}

func (sel selectorRoot) Evaluate(labels map[string]string) bool {
	return sel.EvaluateLabels(MapAsLabels(labels))
}

func (sel selectorRoot) EvaluateLabels(labels Labels) bool {
	return sel.root.Evaluate(labels)
}

func (sel selectorRoot) String() string {
	if sel.cachedString == nil {
		fragments := sel.root.collectFragments([]string{})
		joined := strings.Join(fragments, "")
		sel.cachedString = &joined
	}
	return *sel.cachedString
}

func (sel selectorRoot) UniqueID() string {
	if sel.cachedHash == nil {
		hash := hash.MakeUniqueID("s", sel.String())
		sel.cachedHash = &hash
	}
	return *sel.cachedHash
}

var _ Selector = (*selectorRoot)(nil)

type node interface {
	Evaluate(labels Labels) bool
	collectFragments(fragments []string) []string
}

type LabelEqValueNode struct {
	LabelName string
	Value     string
}

func (node LabelEqValueNode) Evaluate(labels Labels) bool {
	val, ok := labels.Get(node.LabelName)
	if ok {
		return val == node.Value
	}
	return false
}

func (node LabelEqValueNode) collectFragments(fragments []string) []string {
	var quote string
	if strings.Contains(node.Value, `"`) {
		quote = `'`
	} else {
		quote = `"`
	}
	return append(fragments, node.LabelName, " == ", quote, node.Value, quote)
}

type LabelInSetNode struct {
	LabelName string
	Value     StringSet
}

func (node LabelInSetNode) Evaluate(labels Labels) bool {
	val, ok := labels.Get(node.LabelName)
	if ok {
		return node.Value.Contains(val)
	}
	return false
}

func (node LabelInSetNode) collectFragments(fragments []string) []string {
	return collectInOpFragments(fragments, node.LabelName, "in", node.Value)
}

type LabelNotInSetNode struct {
	LabelName string
	Value     StringSet
}

func (node LabelNotInSetNode) Evaluate(labels Labels) bool {
	val, ok := labels.Get(node.LabelName)
	if ok {
		return !node.Value.Contains(val)
	}
	return true
}

func (node LabelNotInSetNode) collectFragments(fragments []string) []string {
	return collectInOpFragments(fragments, node.LabelName, "not in", node.Value)
}

// collectInOpFragments is a shared implementation of collectFragments
// for the 'in' and 'not in' operators.
func collectInOpFragments(fragments []string, labelName, op string, values StringSet) []string {
	var quote string
	fragments = append(fragments, labelName, " ", op, " {")
	first := true
	for _, s := range values {
		if strings.Contains(s, `"`) {
			quote = `'`
		} else {
			quote = `"`
		}
		if !first {
			fragments = append(fragments, ", ")
		} else {
			first = false
		}
		fragments = append(fragments, quote, s, quote)
	}
	fragments = append(fragments, "}")
	return fragments
}

type LabelNeValueNode struct {
	LabelName string
	Value     string
}

func (node LabelNeValueNode) Evaluate(labels Labels) bool {
	val, ok := labels.Get(node.LabelName)
	if ok {
		return val != node.Value
	}
	return true
}

func (node LabelNeValueNode) collectFragments(fragments []string) []string {
	var quote string
	if strings.Contains(node.Value, `"`) {
		quote = `'`
	} else {
		quote = `"`
	}
	return append(fragments, node.LabelName, " != ", quote, node.Value, quote)
}

type HasNode struct {
	LabelName string
}

func (node HasNode) Evaluate(labels Labels) bool {
	_, ok := labels.Get(node.LabelName)
	if ok {
		return true
	}
	return false
}

func (node HasNode) collectFragments(fragments []string) []string {
	return append(fragments, "has(", node.LabelName, ")")
}

type NotNode struct {
	Operand node
}

func (node NotNode) Evaluate(labels Labels) bool {
	return !node.Operand.Evaluate(labels)
}

func (node NotNode) collectFragments(fragments []string) []string {
	fragments = append(fragments, "!")
	return node.Operand.collectFragments(fragments)
}

type AndNode struct {
	Operands []node
}

func (node AndNode) Evaluate(labels Labels) bool {
	for _, operand := range node.Operands {
		if !operand.Evaluate(labels) {
			return false
		}
	}
	return true
}

func (node AndNode) collectFragments(fragments []string) []string {
	fragments = append(fragments, "(")
	fragments = node.Operands[0].collectFragments(fragments)
	for _, op := range node.Operands[1:] {
		fragments = append(fragments, " && ")
		fragments = op.collectFragments(fragments)
	}
	fragments = append(fragments, ")")
	return fragments
}

type OrNode struct {
	Operands []node
}

func (node OrNode) Evaluate(labels Labels) bool {
	for _, operand := range node.Operands {
		if operand.Evaluate(labels) {
			return true
		}
	}
	return false
}

func (node OrNode) collectFragments(fragments []string) []string {
	fragments = append(fragments, "(")
	fragments = node.Operands[0].collectFragments(fragments)
	for _, op := range node.Operands[1:] {
		fragments = append(fragments, " || ")
		fragments = op.collectFragments(fragments)
	}
	fragments = append(fragments, ")")
	return fragments
}

type AllNode struct {
}

func (node AllNode) Evaluate(labels Labels) bool {
	return true
}

func (node AllNode) collectFragments(fragments []string) []string {
	return append(fragments, "all()")
}
