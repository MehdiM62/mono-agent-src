package data

// Companion schema structs (see crypto_schema.go and internal/tools/schemagen)
// for the item-transform nodes data.split_out, data.summarize and
// data.rename_keys. They describe the config keys each Execute reads.

type SplitOutNodeSchema struct {
	FieldToSplitOut string `json:"field_to_split_out" schema:"label=Fields To Split Out,type=text,required,placeholder=items,help=Field holding the list to split; several fields comma-separated. Dot paths reach nested fields (user.addresses). Use $binary to split an item's binary files into separate items."`

	Include string `json:"include" schema:"label=Include,type=select,options=no_other_fields|all_other_fields|selected_other_fields,default=no_other_fields,help=Which other fields of the input item to copy onto each new item."`

	FieldsToInclude string `json:"fields_to_include" schema:"label=Fields To Include,type=text,placeholder=email， name,help=Comma-separated fields copied onto each new item.,depends_on_key=include,depends_on_values=selected_other_fields"`

	DestinationFieldName string `json:"destination_field_name" schema:"label=Destination Field Name,type=text,help=Output field for the split values; one per split field， comma-separated. Empty: object elements are merged into the item and other values keep the field name."`

	DisableDotNotation bool `json:"disable_dot_notation" schema:"label=Disable Dot Notation,type=boolean,default=false,help=Treat field names literally instead of as parent.child paths."`

	IncludeBinary bool `json:"include_binary" schema:"label=Include Binary,type=boolean,default=false,help=Copy the input item's binary data onto every new item."`
}

type SummarizeNodeSchema struct {
	FieldsToSummarize string `json:"fields_to_summarize" schema:"label=Fields To Summarize (JSON),type=textarea,required,rows=5,placeholder=[\n  { \"aggregation\": \"sum\"， \"field\": \"amount\" }，\n  { \"aggregation\": \"count\"， \"field\": \"id\" }\n],help=JSON array of {aggregation， field， include_empty， separate_by， custom_separator}. aggregation: append， average， concatenate， count， count_unique， max， min， sum. Results are named <aggregation>_<field> (e.g. sum_amount， unique_count_id， appended_tags). separate_by applies to concatenate (default '，'; 'other' uses custom_separator)."`

	FieldsToSplitBy string `json:"fields_to_split_by" schema:"label=Fields To Split By,type=text,placeholder=country， city,help=Comma-separated fields to group the summary by."`

	OutputFormat string `json:"output_format" schema:"label=Output Format,type=select,options=separate_items|single_item,default=separate_items,help=separate_items: one item per group. single_item: one item with groups nested by their values."`

	DisableDotNotation bool `json:"disable_dot_notation" schema:"label=Disable Dot Notation,type=boolean,default=false,help=Treat field names literally instead of as parent.child paths."`

	SkipEmptySplitFields bool `json:"skip_empty_split_fields" schema:"label=Skip Items Without Group Value,type=boolean,default=false,help=Leave out items whose group-by field is empty."`
}

type RenameKeysNodeSchema struct {
	Keys string `json:"keys" schema:"label=Keys (JSON),type=textarea,rows=4,placeholder=[\n  { \"current_key\": \"fname\"， \"new_key\": \"first_name\" }\n],help=JSON array of {current_key， new_key}. Dot paths rename nested keys (a.b.c). Applied in order before the regex replacements."`

	RegexReplacements string `json:"regex_replacements" schema:"label=Regex Replacements (JSON),type=textarea,rows=4,placeholder=[\n  { \"search_regex\": \"^old_\"， \"replace_regex\": \"new_\"， \"case_insensitive\": false， \"depth\": -1 }\n],help=JSON array of {search_regex， replace_regex， case_insensitive， depth}. Renames every key matching search_regex (first match per key); $1 refers to a capture group. depth -1 = all levels， 0 = top level only."`
}
