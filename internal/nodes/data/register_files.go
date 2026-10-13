package data

import "github.com/monoes/mono-agent/internal/workflow"

// registerFileNodes registers the file conversion and file system nodes.
func registerFileNodes(r *workflow.NodeTypeRegistry) {
	r.Register("data.convert_to_file", func() workflow.NodeExecutor { return &ConvertToFileNode{} })
	r.Register("data.extract_from_file", func() workflow.NodeExecutor { return &ExtractFromFileNode{} })
	r.Register("data.read_write_file", func() workflow.NodeExecutor { return &ReadWriteFileNode{} })
	r.Register("data.ical", func() workflow.NodeExecutor { return &ICalNode{} })
}
