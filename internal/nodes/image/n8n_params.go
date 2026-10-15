package image

// N8nEditImageParams maps n8n Edit Image (n8n-nodes-base.editImage)
// parameter names to image.edit config keys, for importing n8n workflows.
// Keys inside n8n's "options" collection are dotted ("options.format").
// The same plain names are also accepted inside image.edit's multi_step
// operations list, so steps copied from an n8n multiStep work as they are.
var N8nEditImageParams = map[string]string{
	"operation":                 "operation",
	"dataPropertyName":          "data_property_name",
	"operations":                "operations",
	"blur":                      "blur",
	"sigma":                     "sigma",
	"borderWidth":               "border_width",
	"borderHeight":              "border_height",
	"borderColor":               "border_color",
	"dataPropertyNameComposite": "data_property_name_composite",
	"operator":                  "operator",
	"positionX":                 "position_x",
	"positionY":                 "position_y",
	"backgroundColor":           "background_color",
	"width":                     "width",
	"height":                    "height",
	"resizeOption":              "resize_option",
	"primitive":                 "primitive",
	"color":                     "color",
	"startPositionX":            "start_position_x",
	"startPositionY":            "start_position_y",
	"endPositionX":              "end_position_x",
	"endPositionY":              "end_position_y",
	"cornerRadius":              "corner_radius",
	"rotate":                    "rotate",
	"degreesX":                  "degrees_x",
	"degreesY":                  "degrees_y",
	"text":                      "text",
	"fontSize":                  "font_size",
	"fontColor":                 "font_color",
	"horizontalAlignment":       "horizontal_alignment",
	"verticalAlignment":         "vertical_alignment",
	"lineLength":                "line_length",
	"font":                      "font",
	"options.destinationKey":    "destination_key",
	"options.fileName":          "file_name",
	"options.font":              "font",
	"options.format":            "format",
	"options.quality":           "quality",
}

// N8nEditImageOperations maps n8n Edit Image operation values to
// image.edit's. resizeOption, operator, primitive and the alignment values
// keep n8n's spelling and need no mapping.
var N8nEditImageOperations = map[string]string{
	"blur":        "blur",
	"border":      "border",
	"composite":   "composite",
	"create":      "create",
	"crop":        "crop",
	"draw":        "draw",
	"information": "information",
	"multiStep":   "multi_step",
	"resize":      "resize",
	"rotate":      "rotate",
	"shear":       "shear",
	"text":        "text",
	"transparent": "transparent",
}

// N8nEditImageImportConfig is config an importer should add to every
// image.edit node converted from n8n. n8n renders through GraphicsMagick,
// which reads the last byte of #rrggbbaa as opacity (00 = opaque), the
// reverse of the CSS reading image.edit uses by default.
var N8nEditImageImportConfig = map[string]interface{}{
	"hex_alpha": "opacity",
}
