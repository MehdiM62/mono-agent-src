package data

// ReadWriteFileNodeSchema documents the config keys ReadWriteFileNode.Execute
// reads — see internal/nodes/control.SetNodeSchema's doc comment for why
// this is a companion struct rather than the runtime config, and
// internal/tools/schemagen for the tag grammar.
//
// file_name serves both operations, as in n8n where "fileName" is the
// write target and options.fileName renames what read returns: write uses
// it as the path to write, read as the file name given to every file read.
type ReadWriteFileNodeSchema struct {
	Operation string `json:"operation" schema:"label=Operation,type=select,required,options=read|write,default=read"`

	FileSelector string `json:"file_selector" schema:"label=File(s) Selector,type=text,placeholder=reports/**/*.csv,help=A path or pattern: * and ? within a name， ** across directories， {a，b} alternatives. Relative paths start at the org workdir in confined runs.,depends_on_key=operation,depends_on_values=read"`

	FileName string `json:"file_name" schema:"label=File Name / Path,type=text,placeholder=out/report.csv,help=write: the path to write (parent directories are created). read: overrides the file name of the files read."`

	DataPropertyName string `json:"data_property_name" schema:"label=Binary Property,type=text,default=data,help=read: the property each file is put in. write: the property holding the file to write."`

	Append bool `json:"append" schema:"label=Append,type=boolean,default=false,help=Append to the file instead of replacing it.,depends_on_key=operation,depends_on_values=write"`

	MimeType string `json:"mime_type" schema:"label=MIME Type,type=text,placeholder=application/zip,help=Overrides the detected MIME type of the files read.,depends_on_key=operation,depends_on_values=read"`

	FileExtension string `json:"file_extension" schema:"label=File Extension,type=text,placeholder=zip,help=Overrides the extension of the files read.,depends_on_key=operation,depends_on_values=read"`

	LiteralBrackets bool `json:"literal_brackets" schema:"label=Treat Brackets as Literal,type=boolean,default=true,help=Match [ and ] as characters. Turn off to use them as character classes like [0-9].,depends_on_key=operation,depends_on_values=read"`
}
