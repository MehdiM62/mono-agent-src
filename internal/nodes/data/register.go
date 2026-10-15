package data

import "github.com/monoes/mono-agent/internal/workflow"

func RegisterAll(r *workflow.NodeTypeRegistry) {
	r.Register("data.datetime", func() workflow.NodeExecutor { return &DateTimeNode{} })
	r.Register("data.crypto", func() workflow.NodeExecutor { return &CryptoNode{} })
	r.Register("data.html", func() workflow.NodeExecutor { return &HTMLNode{} })
	r.Register("data.xml", func() workflow.NodeExecutor { return &XMLNode{} })
	r.Register("data.markdown", func() workflow.NodeExecutor { return &MarkdownNode{} })
	r.Register("data.spreadsheet", func() workflow.NodeExecutor { return &SpreadsheetNode{} })
	r.Register("data.compression", func() workflow.NodeExecutor { return &CompressionNode{} })
	r.Register("data.write_binary_file", func() workflow.NodeExecutor { return &WriteBinaryFileNode{} })
	r.Register("data.split_out", func() workflow.NodeExecutor { return &SplitOutNode{} })
	r.Register("data.summarize", func() workflow.NodeExecutor { return &SummarizeNode{} })
	r.Register("data.rename_keys", func() workflow.NodeExecutor { return &RenameKeysNode{} })
	r.Register("data.jwt", func() workflow.NodeExecutor { return &JWTNode{} })
	r.Register("data.totp", func() workflow.NodeExecutor { return &TOTPNode{} })
	r.Register("data.debug_helper", func() workflow.NodeExecutor { return &DebugHelperNode{} })
	registerFileNodes(r)
	registerFormatNodes(r)
}
