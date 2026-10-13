package data

// n8n parameter maps for the n8n workflow importer.
//
// Each N8n<Node>Params map takes an n8n node parameter name to the config key
// of the mono node that replaces it. Options nested in an n8n "collection"
// or "fixedCollection" parameter use a dotted name ("options.timezone"); the
// importer flattens them onto our flat config. A missing entry means the
// parameter has no counterpart (the importer should report it, not guess).
//
// N8n<Node>Values maps n8n option values to ours, per mono config key, for
// the few options whose values were renamed to snake_case. Values not listed
// pass through unchanged (every node also accepts n8n's camelCase spelling of
// the renamed values at run time).
//
// Lists of objects (Summarize's fieldsToSummarize.values, Rename Keys'
// keys.key, …) keep their element shape; only the element field names change,
// given by the N8n<Node>ListFields maps (n8n element field → ours).

// N8nNodeTypes maps n8n node type names to the mono node type replacing them.
// Item Lists (n8n-nodes-base.itemLists) is per operation: see
// N8nItemListsOperations.
var N8nNodeTypes = map[string]string{
	"n8n-nodes-base.splitOut":         "data.split_out",
	"n8n-nodes-base.summarize":        "data.summarize",
	"n8n-nodes-base.renameKeys":       "data.rename_keys",
	"n8n-nodes-base.dateTime":         "data.datetime",
	"n8n-nodes-base.crypto":           "data.crypto",
	"n8n-nodes-base.jwt":              "data.jwt",
	"n8n-nodes-base.totp":             "data.totp",
	"n8n-nodes-base.debugHelper":      "data.debug_helper",
	"n8n-nodes-base.limit":            "core.limit",
	"n8n-nodes-base.sort":             "core.sort",
	"n8n-nodes-base.removeDuplicates": "core.remove_duplicates",
	"n8n-nodes-base.aggregate":        "core.aggregate",
	// Mapped in n8n_params_files.go and n8n_params_formats.go; Edit Image in
	// internal/nodes/image/n8n_params.go.
	"n8n-nodes-base.convertToFile":   "data.convert_to_file",
	"n8n-nodes-base.extractFromFile": "data.extract_from_file",
	"n8n-nodes-base.readWriteFile":   "data.read_write_file",
	"n8n-nodes-base.iCal":            "data.ical",
	"n8n-nodes-base.compression":     "data.compression",
	"n8n-nodes-base.xml":             "data.xml",
	"n8n-nodes-base.html":            "data.html",
	"n8n-nodes-base.markdown":        "data.markdown",
	"n8n-nodes-base.editImage":       "image.edit",
}

// ── Split Out ────────────────────────────────────────────────────────────────

var N8nSplitOutParams = map[string]string{
	"fieldToSplitOut":              "field_to_split_out",
	"include":                      "include",
	"fieldsToInclude":              "fields_to_include",
	"options.destinationFieldName": "destination_field_name",
	"options.disableDotNotation":   "disable_dot_notation",
	"options.includeBinary":        "include_binary",
}

var N8nSplitOutValues = map[string]map[string]string{
	"include": {"noOtherFields": "no_other_fields", "allOtherFields": "all_other_fields", "selectedOtherFields": "selected_other_fields"},
}

// ── Summarize ────────────────────────────────────────────────────────────────

var N8nSummarizeParams = map[string]string{
	"fieldsToSummarize.values":        "fields_to_summarize",
	"fieldsToSplitBy":                 "fields_to_split_by",
	"options.outputFormat":            "output_format",
	"options.disableDotNotation":      "disable_dot_notation",
	"options.skipEmptySplitFields":    "skip_empty_split_fields",
	"options.continueIfFieldNotFound": "", // always continues in mono
}

var N8nSummarizeListFields = map[string]string{
	"aggregation":     "aggregation",
	"field":           "field",
	"includeEmpty":    "include_empty",
	"separateBy":      "separate_by",
	"customSeparator": "custom_separator",
}

var N8nSummarizeValues = map[string]map[string]string{
	"output_format": {"separateItems": "separate_items", "singleItem": "single_item"},
	"aggregation":   {"countUnique": "count_unique"},
}

// ── Rename Keys ──────────────────────────────────────────────────────────────

var N8nRenameKeysParams = map[string]string{
	"keys.key": "keys",
	"additionalOptions.regexReplacement.replacements": "regex_replacements",
}

var N8nRenameKeysListFields = map[string]string{
	"currentKey":              "current_key",
	"newKey":                  "new_key",
	"searchRegex":             "search_regex",
	"replaceRegex":            "replace_regex",
	"options.caseInsensitive": "case_insensitive",
	"options.depth":           "depth",
}

// ── Item Lists (v3) ──────────────────────────────────────────────────────────

// N8nItemListsOperations maps each Item Lists operation to the mono node
// that performs it. Item Lists is a bundle of the stand-alone transform
// nodes, so there is no data.item_lists.
var N8nItemListsOperations = map[string]string{
	"concatenateItems": "core.aggregate",
	"aggregateItems":   "core.aggregate",
	"limit":            "core.limit",
	"removeDuplicates": "core.remove_duplicates",
	"sort":             "core.sort",
	"splitOutItems":    "data.split_out",
	"summarize":        "data.summarize",
}

// N8nItemListsParams gives the parameter map per Item Lists operation.
// core.limit keeps only the first items (n8n's keep=lastItems has no
// counterpart); core.sort sorts by one field (the first sortFieldsUi entry;
// random and code sorting have no counterpart); core.remove_duplicates
// compares one field or the whole item; core.aggregate's "collect"
// operation covers aggregateIndividualFields for one field.
var N8nItemListsParams = map[string]map[string]string{
	"limit": {"maxItems": "max_items"},
	"sort": {
		"sortFieldsUi.sortField[0].fieldName": "field",
		"sortFieldsUi.sortField[0].order":     "direction",
	},
	"removeDuplicates": {"fieldsToCompare": "field"},
	"concatenateItems": {
		"fieldsToAggregate.fieldToAggregate[0].fieldToAggregate": "field",
		"fieldsToAggregate.fieldToAggregate[0].outputFieldName":  "output_field",
	},
	"splitOutItems": N8nSplitOutParams,
	"summarize":     N8nSummarizeParams,
}

var N8nItemListsValues = map[string]map[string]string{
	"direction": {"ascending": "asc", "descending": "desc"},
}

// ── Date & Time (v2) ─────────────────────────────────────────────────────────

var N8nDateTimeParams = map[string]string{
	"operation":                  "operation",
	"includeTime":                "include_time",
	"outputFieldName":            "output_field_name",
	"options.timezone":           "timezone",
	"options.includeInputFields": "include_input_fields",
	"magnitude":                  "magnitude",
	"timeUnit":                   "time_unit",
	"duration":                   "duration",
	"date":                       "date",
	"format":                     "format",
	"customFormat":               "custom_format",
	"options.fromFormat":         "from_format",
	"mode":                       "mode",
	"toNearest":                  "to_nearest",
	"to":                         "to",
	"startDate":                  "start_date",
	"endDate":                    "end_date",
	"units":                      "units",
	"options.isoString":          "iso_string",
	"part":                       "part",
}

// N8nDateTimeOperations maps n8n's v2 operation values to ours.
var N8nDateTimeOperations = map[string]string{
	"getCurrentDate":      "get_current_date",
	"addToDate":           "add_to_date",
	"subtractFromDate":    "subtract_from_date",
	"formatDate":          "format_date",
	"roundDate":           "round_date",
	"getTimeBetweenDates": "get_time_between_dates",
	"extractDate":         "extract_date",
}

var N8nDateTimeValues = map[string]map[string]string{
	"operation": N8nDateTimeOperations,
	"mode":      {"roundDown": "round_down", "roundUp": "round_up"},
}

// ── Crypto (v2) ──────────────────────────────────────────────────────────────

// N8nCryptoParams: n8n keeps keys and secrets in a "crypto" credential
// (hmacSecret, signPrivateKey, encryptionPassphrase, encryptionPublicKey,
// encryptionPrivateKey); they map to secret, private_key, passphrase,
// public_key and private_key, normally as @secret: references.
var N8nCryptoParams = map[string]string{
	"action":             "operation",
	"type":               "type",
	"value":              "value",
	"dataPropertyName":   "data_property_name",
	"encoding":           "encoding",
	"binaryData":         "binary_data",
	"binaryPropertyName": "binary_property_name",
	"algorithm":          "algorithm",
	"encodingType":       "encoding_type",
	"stringLength":       "string_length",
	"mode":               "mode",
	"cipher":             "cipher",
	"oaepHash":           "oaep_hash",
}

var N8nCryptoCredentialFields = map[string]string{
	"hmacSecret":           "secret",
	"signPrivateKey":       "private_key",
	"encryptionPassphrase": "passphrase",
	"encryptionPublicKey":  "public_key",
	"encryptionPrivateKey": "private_key",
}

// ── JWT ──────────────────────────────────────────────────────────────────────

var N8nJwtParams = map[string]string{
	"operation":                "operation",
	"useJson":                  "use_json",
	"claims":                   "claims",
	"claims.audience":          "claims.audience",
	"claims.expiresIn":         "claims.expires_in",
	"claims.issuer":            "claims.issuer",
	"claims.jwtid":             "claims.jwtid",
	"claims.notBefore":         "claims.not_before",
	"claims.subject":           "claims.subject",
	"claimsJson":               "claims_json",
	"headerClaims":             "header_claims",
	"token":                    "token",
	"options.complete":         "complete",
	"options.ignoreExpiration": "ignore_expiration",
	"options.ignoreNotBefore":  "ignore_not_before",
	"options.clockTolerance":   "clock_tolerance",
	"options.kid":              "kid",
	"options.algorithm":        "algorithm",
}

// N8nJwtCredentialFields maps the jwtAuth credential's fields.
var N8nJwtCredentialFields = map[string]string{
	"keyType":    "key_type",
	"secret":     "secret",
	"privateKey": "private_key",
	"publicKey":  "public_key",
	"algorithm":  "algorithm",
}

var N8nJwtValues = map[string]map[string]string{
	"key_type": {"pemKey": "pem_key"},
}

// ── TOTP ─────────────────────────────────────────────────────────────────────

var N8nTotpParams = map[string]string{
	"operation":         "operation",
	"options.algorithm": "algorithm",
	"options.digits":    "digits",
	"options.period":    "period",
}

// N8nTotpCredentialFields maps the totpApi credential's fields.
var N8nTotpCredentialFields = map[string]string{"secret": "secret", "label": "label"}

var N8nTotpValues = map[string]map[string]string{
	"operation": {"generateSecret": "generate_secret"},
}

// ── Debug Helper ─────────────────────────────────────────────────────────────

var N8nDebugHelperParams = map[string]string{
	"category":              "category",
	"throwErrorType":        "throw_error_type",
	"throwErrorMessage":     "throw_error_message",
	"randomDataType":        "random_data_type",
	"randomDataCount":       "random_data_count",
	"randomDataSeed":        "random_data_seed",
	"randomDataSingleArray": "random_data_single_array",
	"nanoidAlphabet":        "nanoid_alphabet",
	"nanoidLength":          "nanoid_length",
	"memorySizeValue":       "", // out-of-memory simulation is not supported
}

var N8nDebugHelperValues = map[string]map[string]string{
	"category":         debugCategoryAliases,
	"random_data_type": randomTypeAliases,
}
