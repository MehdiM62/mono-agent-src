package data

// CompressionNodeSchema documents the config keys CompressionNode.Execute
// reads out of its map[string]interface{} config — see
// internal/nodes/control.SetNodeSchema's doc comment for why this is a
// companion struct rather than the runtime config, and
// internal/tools/schemagen for the tag grammar.
//
// "filename" has no entry in the hand-written schemas/data.compression.json
// this replaces — Execute reads it (defaulting to "data") to name the entry
// written inside the zip archive for operation=zip. The hand-written schema
// never exposed a way to set it.
//
// "compress" and "decompress" are the n8n Compression node's operations
// (compression_files.go): they work on binary properties (base64 fields
// with file metadata, see binary.go) rather than on one raw field, and
// handle several files and archive formats.
type CompressionNodeSchema struct {
	Operation string `json:"operation" schema:"label=Operation,type=select,required,options=zip|unzip|gzip|gunzip|compress|decompress,default=zip,help=zip/unzip/gzip/gunzip work on one base64 field. compress/decompress work on binary properties (files) and several archive formats."`

	InputField string `json:"input_field" schema:"label=Input Field,type=text,required,placeholder=file_data,help=Item field containing the base64-encoded input data.,depends_on_key=operation,depends_on_values=zip|unzip|gzip|gunzip"`

	OutputField string `json:"output_field" schema:"label=Output Field Name,type=text,default=compressed,depends_on_key=operation,depends_on_values=zip|unzip|gzip|gunzip"`

	Filename string `json:"filename" schema:"label=Archive Entry Filename,type=text,default=data,help=Name given to the entry inside the zip archive (operation=zip only).,depends_on_key=operation,depends_on_values=zip"`

	BinaryPropertyName string `json:"binary_property_name" schema:"label=Input Binary Properties,type=text,default=data,placeholder=data，attachment,help=Comma-separated binary properties to compress or decompress.,depends_on_key=operation,depends_on_values=compress|decompress"`

	OutputFormat string `json:"output_format" schema:"label=Output Format,type=select,options=zip|gzip|tar|targz,default=zip,help=zip/tar/targz pack every input file into one archive; gzip compresses each file on its own.,depends_on_key=operation,depends_on_values=compress"`

	FileName string `json:"file_name" schema:"label=Output File Name,type=text,placeholder=archive.zip,help=Name of the archive (zip/tar/targz) or base name of each .gz file. Defaults to archive.<ext> / the input file name.,depends_on_key=operation,depends_on_values=compress"`

	BinaryPropertyOutput string `json:"binary_property_output" schema:"label=Output Binary Property,type=text,default=data,help=Binary property for the archive. With gzip and several inputs the second file goes to <name>1， the third to <name>2 ….,depends_on_key=operation,depends_on_values=compress"`

	OutputPrefix string `json:"output_prefix" schema:"label=Output Prefix,type=text,default=file_,help=Extracted files go to binary properties <prefix>0， <prefix>1 ….,depends_on_key=operation,depends_on_values=decompress"`
}
