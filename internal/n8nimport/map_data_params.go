package n8nimport

import (
	datanodes "github.com/monoes/mono-agent/internal/nodes/data"
	imagenodes "github.com/monoes/mono-agent/internal/nodes/image"
)

// n8n nodes converted through the parameter tables of internal/nodes/data
// and internal/nodes/image: the mono node implements the n8n node's
// options, so the import is a rename of its parameters.

func init() {
	for n8nType, spec := range dataParamSpecs {
		spec := spec
		Register(n8nType, func(c *MapContext) Mapping { return mapWithParams(c, spec) })
	}
	Register("n8n-nodes-base.splitOut", mapSplitOut)
	Register("n8n-nodes-base.summarize", mapSummarize)
	Register("n8n-nodes-base.dateTime", mapDateTime)
	Register("n8n-nodes-base.crypto", mapCrypto)
	Register("n8n-nodes-base.jwt", mapJWT)
	Register("n8n-nodes-base.totp", mapTOTP)
	Register("n8n-nodes-base.iCal", mapICal)
}

func opValues(m map[string]string) map[string]map[string]string {
	return map[string]map[string]string{"operation": m}
}

// dataParamSpecs are the nodes that need nothing beyond their tables.
var dataParamSpecs = map[string]paramSpec{
	"n8n-nodes-base.renameKeys": {Type: "data.rename_keys", Params: datanodes.N8nRenameKeysParams,
		ListFields: map[string]map[string]string{"keys": datanodes.N8nRenameKeysListFields, "regex_replacements": datanodes.N8nRenameKeysListFields}},
	"n8n-nodes-base.debugHelper": {Type: "data.debug_helper", Params: datanodes.N8nDebugHelperParams, Values: datanodes.N8nDebugHelperValues},
	"n8n-nodes-base.convertToFile": {Type: "data.convert_to_file", Params: datanodes.N8nConvertToFileParams,
		Values: opValues(datanodes.N8nConvertToFileOperations), Defaults: map[string]interface{}{"operation": "csv"}},
	"n8n-nodes-base.extractFromFile": {Type: "data.extract_from_file", Params: datanodes.N8nExtractFromFileParams,
		Values: opValues(datanodes.N8nExtractFromFileOperations), Defaults: map[string]interface{}{"operation": "csv"}},
	"n8n-nodes-base.readWriteFile": {Type: "data.read_write_file", Params: datanodes.N8nReadWriteFileParams,
		Defaults: map[string]interface{}{"operation": "read"}},
	"n8n-nodes-base.compression": {Type: "data.compression", Params: datanodes.N8nCompressionParams,
		Defaults: map[string]interface{}{"operation": "decompress"}},
	"n8n-nodes-base.xml": {Type: "data.xml", Params: datanodes.N8nXmlParams,
		Values: opValues(datanodes.N8nXmlModes), Defaults: map[string]interface{}{"mode": "xmlToJson"}},
	"n8n-nodes-base.html": {Type: "data.html", Params: datanodes.N8nHtmlParams,
		Values:     opValues(datanodes.N8nHtmlOperations),
		ListFields: map[string]map[string]string{"extraction_values": datanodes.N8nHtmlExtractionValueKeys},
		Defaults:   map[string]interface{}{"operation": "generateHtmlTemplate"}},
	"n8n-nodes-base.markdown": {Type: "data.markdown", Params: datanodes.N8nMarkdownParams,
		Values:   map[string]map[string]string{"mode": datanodes.N8nMarkdownModes},
		Defaults: map[string]interface{}{"mode": "htmlToMarkdown"}},
	"n8n-nodes-base.editImage": {Type: "image.edit", Params: imagenodes.N8nEditImageParams,
		Values: opValues(imagenodes.N8nEditImageOperations), Fixed: imagenodes.N8nEditImageImportConfig,
		Defaults: map[string]interface{}{"operation": "border"}},
}

var (
	splitOutSpec  = paramSpec{Type: "data.split_out", Params: datanodes.N8nSplitOutParams, Values: datanodes.N8nSplitOutValues}
	summarizeSpec = paramSpec{Type: "data.summarize", Params: datanodes.N8nSummarizeParams, Values: datanodes.N8nSummarizeValues,
		ListFields: map[string]map[string]string{"fields_to_summarize": datanodes.N8nSummarizeListFields}}
)

func mapSplitOut(c *MapContext) Mapping  { return mapWithParams(c, splitOutSpec) }
func mapSummarize(c *MapContext) Mapping { return mapWithParams(c, summarizeSpec) }

// mapDateTime: version 2 maps onto data.datetime; version 1 runs as code.
func mapDateTime(c *MapContext) Mapping {
	if !c.version(2) {
		return dateTimeV1(c)
	}
	return mapWithParams(c, paramSpec{Type: "data.datetime", Params: datanodes.N8nDateTimeParams,
		Values: datanodes.N8nDateTimeValues, Defaults: map[string]interface{}{"operation": "getCurrentDate"}})
}

// mapCrypto maps every Crypto version onto data.crypto's n8n actions. The
// keys n8n keeps in a "crypto" credential come from vault secrets.
func mapCrypto(c *MapContext) Mapping {
	typ := "MD5"
	if c.version(2) {
		typ = "SHA256"
	}
	m := mapWithParams(c, paramSpec{Type: "data.crypto", Params: datanodes.N8nCryptoParams,
		Defaults: map[string]interface{}{"action": "hash", "type": typ}})
	switch m.Config["operation"] {
	case "hmac":
		if c.Has("secret") {
			m.Config["secret"] = c.Str("secret", "")
		} else {
			m.Config["secret"] = c.Secret("secret", "crypto")
		}
	case "sign":
		m.Config["private_key"] = c.Secret("private_key", "crypto")
	case "encrypt", "decrypt":
		c.Note("set passphrase, public_key or private_key from the n8n crypto credential (as @secret: references)")
	}
	return m
}

// mapJWT: the jwtAuth credential's secret comes from a vault secret.
func mapJWT(c *MapContext) Mapping {
	m := mapWithParams(c, paramSpec{Type: "data.jwt", Params: datanodes.N8nJwtParams, Values: datanodes.N8nJwtValues,
		Defaults: map[string]interface{}{"operation": "sign"}})
	if m.Config["operation"] != "decode" {
		m.Config["secret"] = c.Secret("secret", "jwtAuth")
		c.Note("a PEM-key jwtAuth credential needs key_type pem_key and private_key / public_key instead of secret")
	}
	return m
}

// mapTOTP: the totpApi credential's secret comes from a vault secret.
func mapTOTP(c *MapContext) Mapping {
	m := mapWithParams(c, paramSpec{Type: "data.totp", Params: datanodes.N8nTotpParams, Values: datanodes.N8nTotpValues,
		Defaults: map[string]interface{}{"operation": "generateSecret"}})
	m.Config["secret"] = c.Secret("secret", "totpApi")
	return m
}

// mapICal: data.ical is n8n's only iCalendar operation (createEventFile).
func mapICal(c *MapContext) Mapping {
	c.Ignore("operation", "additionalFields.useWorkflowTimezone")
	return mapWithParams(c, paramSpec{Type: "data.ical", Params: datanodes.N8nICalendarParams})
}
