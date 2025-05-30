// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"github.com/aws/amazon-cloudwatch-agent-operator/cmd/amazon-cloudwatch-agent-target-allocator/allocation"
	"github.com/aws/amazon-cloudwatch-agent-operator/cmd/amazon-cloudwatch-agent-target-allocator/target"
)

var _ allocation.Allocator = &mockAllocator{}

// mockAllocator implements the Allocator interface, but all funcs other than
// TargetItems() are a no-op.
type mockAllocator struct {
	targetItems map[string]*target.Item
}

func (m *mockAllocator) SetCollectors(_ map[string]*allocation.Collector)               {}
func (m *mockAllocator) SetTargets(_ map[string]*target.Item)                           {}
func (m *mockAllocator) Collectors() map[string]*allocation.Collector                   { return nil }
func (m *mockAllocator) GetTargetsForCollectorAndJob(_ string, _ string) []*target.Item { return nil }
func (m *mockAllocator) SetFilter(_ allocation.Filter)                                  {}

func (m *mockAllocator) TargetItems() map[string]*target.Item {
	return m.targetItems
}
