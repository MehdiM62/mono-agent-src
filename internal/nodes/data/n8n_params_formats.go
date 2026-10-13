package data

// n8n → mono parameter maps for the XML, HTML and Markdown nodes, for
// importing n8n workflows.
//
// Each N8n*Params map goes from an n8n parameter name to the mono config
// key that holds the same setting. Parameters inside an n8n "options"
// collection are written as dotted paths ("options.attrkey"); mono flattens
// those collections into top-level keys. A fixedCollection whose entries
// sit under an inner name is mapped by its full path
// ("extractionValues.values"); the mono key then holds the list itself
// (the nodes also accept the n8n wrapper object as-is).
//
// The N8n*Operations / N8n*Modes maps translate the values of the
// operation/mode parameter where the names differ. n8n parameters with no
// entry have no mono equivalent (each node file lists those it skips).
var (
	// N8nXmlParams maps n8n-nodes-base.xml parameters. n8n's "mode" is the
	// mono "operation".
	N8nXmlParams = map[string]string{
		"mode":                        "operation",
		"dataPropertyName":            "data_property_name",
		"options.attrkey":             "attrkey",
		"options.charkey":             "charkey",
		"options.explicitArray":       "explicit_array",
		"options.explicitRoot":        "explicit_root",
		"options.ignoreAttrs":         "ignore_attrs",
		"options.mergeAttrs":          "merge_attrs",
		"options.normalize":           "normalize",
		"options.normalizeTags":       "normalize_tags",
		"options.trim":                "trim",
		"options.rootName":            "root_name",
		"options.headless":            "headless",
		"options.cdata":               "cdata",
		"options.allowSurrogateChars": "allow_surrogate_chars",
	}

	// N8nXmlModes maps n8n XML "mode" values to mono operations.
	N8nXmlModes = map[string]string{
		"xmlToJson": "xml_to_json",
		"jsonToxml": "json_to_xml",
	}

	// N8nHtmlParams maps n8n-nodes-base.html (v1.2) parameters.
	N8nHtmlParams = map[string]string{
		"operation":                "operation",
		"html":                     "html",
		"sourceData":               "source_data",
		"dataPropertyName":         "data_property_name",
		"extractionValues.values":  "extraction_values",
		"options.trimValues":       "trim_values",
		"options.cleanUpText":      "clean_up_text",
		"options.capitalize":       "capitalize",
		"options.customStyling":    "custom_styling",
		"options.caption":          "caption",
		"options.tableAttributes":  "table_attributes",
		"options.headerAttributes": "header_attributes",
		"options.rowAttributes":    "row_attributes",
		"options.cellAttributes":   "cell_attributes",
	}

	// N8nHtmlExtractionValueKeys maps the keys of one extractionValues
	// entry. The node also reads the n8n spellings directly.
	N8nHtmlExtractionValueKeys = map[string]string{
		"key":           "key",
		"cssSelector":   "css_selector",
		"returnValue":   "return_value",
		"attribute":     "attribute",
		"returnArray":   "return_array",
		"skipSelectors": "skip_selectors",
	}

	// N8nHtmlOperations maps n8n HTML "operation" values to mono operations.
	N8nHtmlOperations = map[string]string{
		"generateHtmlTemplate": "generate_html_template",
		"extractHtmlContent":   "extract_html_content",
		"convertToHtmlTable":   "convert_to_html_table",
	}

	// N8nMarkdownParams maps n8n-nodes-base.markdown parameters. The two
	// modes share the "options" name in n8n but their option names do not
	// overlap, so one map covers both. Not mapped (no goldmark equivalent):
	// options.emoji, encodeEmails, ghCodeBlocks, ghMentions,
	// ghMentionsLink, parseImgDimensions, tablesHeaderId,
	// backslashEscapesHTMLTags, literalMidWordAsterisks,
	// literalMidWordUnderscores, requireSpaceBeforeHeadingText,
	// smartIndentationFix, disableForced4SpacesIndentedSublists,
	// splitAdjacentBlockquotes, excludeTrailingPunctuationFromURLs.
	N8nMarkdownParams = map[string]string{
		"mode":           "mode",
		"html":           "html",
		"markdown":       "markdown",
		"destinationKey": "destination_key",
		// html_to_markdown
		"options.bulletMarker":                "bullet_marker",
		"options.codeBlockStyle":              "code_block_style",
		"options.codeFence":                   "code_fence",
		"options.emDelimiter":                 "em_delimiter",
		"options.strongDelimiter":             "strong_delimiter",
		"options.globalEscape.value":          "global_escape",
		"options.lineStartEscape.value":       "line_start_escape",
		"options.textReplace.values":          "text_replace",
		"options.ignore":                      "ignore",
		"options.blockElements":               "block_elements",
		"options.keepDataImages":              "keep_data_images",
		"options.maxConsecutiveNewlines":      "max_consecutive_newlines",
		"options.useLinkReferenceDefinitions": "use_link_reference_definitions",
		// markdown_to_html
		"options.tables":               "tables",
		"options.strikethrough":        "strikethrough",
		"options.tasklists":            "tasklists",
		"options.simpleLineBreaks":     "simple_line_breaks",
		"options.simplifiedAutoLink":   "simplified_auto_link",
		"options.noHeaderId":           "no_header_id",
		"options.customizedHeaderId":   "customized_header_id",
		"options.ghCompatibleHeaderId": "gh_compatible_header_id",
		"options.rawHeaderId":          "raw_header_id",
		"options.prefixHeaderId":       "prefix_header_id",
		"options.rawPrefixHeaderId":    "raw_prefix_header_id",
		"options.headerLevelStart":     "header_level_start",
		"options.openLinksInNewWindow": "open_links_in_new_window",
		"options.completeHTMLDocument": "complete_html_document",
	}

	// N8nMarkdownModes maps n8n Markdown "mode" values to mono modes.
	N8nMarkdownModes = map[string]string{
		"markdownToHtml": "markdown_to_html",
		"htmlToMarkdown": "html_to_markdown",
	}
)
