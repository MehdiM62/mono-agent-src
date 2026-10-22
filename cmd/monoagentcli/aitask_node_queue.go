package main

import (
	aitaskcli "github.com/monoes/mono-agent/internal/cli/aitask"
	aitasknodes "github.com/monoes/mono-agent/internal/nodes/aitask"
)

// The ai.task workflow node queues through this binary's launcher;
// internal/nodes/aitask cannot import internal/cli/aitask itself.
func init() { aitasknodes.Queue = aitaskcli.Queue }
