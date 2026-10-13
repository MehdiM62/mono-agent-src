package image

import (
	"fmt"
	"image/color"
	"strings"
)

// Operation names image.edit accepts, as written in config.
const (
	opBlur        = "blur"
	opBorder      = "border"
	opComposite   = "composite"
	opCreate      = "create"
	opCrop        = "crop"
	opDraw        = "draw"
	opInformation = "information"
	opMultiStep   = "multi_step"
	opResize      = "resize"
	opRotate      = "rotate"
	opShear       = "shear"
	opText        = "text"
	opTransparent = "transparent"
)

var editOperations = []string{opBlur, opBorder, opComposite, opCreate, opCrop, opDraw,
	opInformation, opMultiStep, opResize, opRotate, opShear, opText, opTransparent}

var resizeOptions = []string{"ignoreAspectRatio", "maximumArea", "minimumArea", "onlyIfLarger", "onlyIfSmaller", "percent"}

var compositeOperators = []string{"Add", "Atop", "Bumpmap", "Copy", "CopyBlack", "CopyBlue",
	"CopyCyan", "CopyGreen", "CopyMagenta", "CopyOpacity", "CopyRed", "CopyYellow", "Difference",
	"Divide", "In", "Minus", "Multiply", "Out", "Over", "Plus", "Subtract", "Xor"}

// editOp is one parsed pixel operation. Only the fields of its Name are set.
// Defaults equal n8n's Edit Image defaults so that imported workflows, which
// omit parameters left at their default, behave the same.
type editOp struct {
	Name string

	Blur, Sigma float64 // blur

	BorderWidth, BorderHeight int // border
	BorderColor               color.NRGBA

	CompositeProp        string // composite
	Operator             string
	PositionX, PositionY int // composite, crop, text

	Background    color.NRGBA // create, rotate, shear
	Width, Height float64     // create, crop, resize
	ResizeOption  string

	Primitive          string // draw
	Color              color.NRGBA
	StartX, StartY     int
	EndX, EndY         int
	CornerRadius       int
	Rotate             float64 // rotate
	DegreesX, DegreesY float64 // shear
	Fuzz               float64 // transparent
	Text               string  // text
	FontSize           float64
	FontColor          color.NRGBA
	HAlign, VAlign     string
	LineLength         int
	Font               string
}

var (
	opaqueBlack = color.NRGBA{0, 0, 0, 255}
	opaqueWhite = color.NRGBA{255, 255, 255, 255}
	opaqueRed   = color.NRGBA{255, 0, 0, 255}
	clearWhite  = color.NRGBA{255, 255, 255, 0}
)

// parseOp parses the parameters of operation name out of m. Errors name
// the offending key.
func parseOp(m map[string]interface{}, name string, mode hexAlphaMode) (editOp, error) {
	op := editOp{Name: name}
	var err error
	num := func(key string, def float64) float64 {
		if err != nil {
			return 0
		}
		var v float64
		v, err = cfgNum(m, key, def)
		return v
	}
	integer := func(key string, def int) int {
		if err != nil {
			return 0
		}
		var v int
		v, err = cfgInt(m, key, def)
		return v
	}
	col := func(key string, def color.NRGBA) color.NRGBA {
		if err != nil {
			return def
		}
		var v color.NRGBA
		v, err = cfgColor(m, key, def, mode)
		return v
	}
	choice := func(key, def string, allowed ...string) string {
		if err != nil {
			return ""
		}
		var v string
		v, err = cfgChoice(m, key, def, allowed...)
		return v
	}

	switch name {
	case opBlur:
		op.Blur, op.Sigma = num("blur", 5), num("sigma", 2)
		if err == nil && (op.Blur < 0 || op.Sigma < 0) {
			err = fmt.Errorf("blur/sigma: must not be negative")
		}
	case opBorder:
		op.BorderWidth, op.BorderHeight = integer("border_width", 10), integer("border_height", 10)
		op.BorderColor = col("border_color", opaqueBlack)
		if err == nil && (op.BorderWidth < 0 || op.BorderHeight < 0) {
			err = fmt.Errorf("border_width/border_height: must not be negative")
		}
	case opComposite:
		op.CompositeProp = strings.TrimSpace(cfgStr(m, "data_property_name_composite", ""))
		op.Operator = choice("operator", "Over", compositeOperators...)
		op.PositionX, op.PositionY = integer("position_x", 0), integer("position_y", 0)
		if err == nil && op.CompositeProp == "" {
			err = fmt.Errorf("data_property_name_composite: required (the binary property or path field of the image to lay over)")
		}
	case opCreate:
		op.Background = col("background_color", opaqueWhite)
		op.Width, op.Height = num("width", 50), num("height", 50)
		if err == nil && (op.Width < 1 || op.Height < 1) {
			err = fmt.Errorf("width/height: must be at least 1")
		}
	case opCrop:
		op.Width, op.Height = num("width", 500), num("height", 500)
		op.PositionX, op.PositionY = integer("position_x", 0), integer("position_y", 0)
		if err == nil && (op.Width < 1 || op.Height < 1) {
			err = fmt.Errorf("width/height: must be at least 1")
		}
	case opDraw:
		op.Primitive = choice("primitive", "rectangle", "circle", "line", "rectangle")
		op.Color = col("color", opaqueRed)
		op.StartX, op.StartY = integer("start_position_x", 50), integer("start_position_y", 50)
		op.EndX, op.EndY = integer("end_position_x", 250), integer("end_position_y", 250)
		op.CornerRadius = integer("corner_radius", 0)
		if err == nil && op.CornerRadius < 0 {
			err = fmt.Errorf("corner_radius: must not be negative")
		}
	case opResize:
		op.Width, op.Height = num("width", 500), num("height", 500)
		op.ResizeOption = choice("resize_option", "maximumArea", resizeOptions...)
		if err == nil && (op.Width < 0 || op.Height < 0 || (op.Width == 0 && op.Height == 0)) {
			err = fmt.Errorf("width/height: must not be negative and at least one must be set")
		}
	case opRotate:
		op.Rotate = num("rotate", 0)
		op.Background = col("background_color", clearWhite)
	case opShear:
		op.DegreesX, op.DegreesY = num("degrees_x", 0), num("degrees_y", 0)
		op.Background = col("background_color", clearWhite)
		if err == nil && (abs(op.DegreesX) >= 90 || abs(op.DegreesY) >= 90) {
			err = fmt.Errorf("degrees_x/degrees_y: must be between -90 and 90 (exclusive)")
		}
	case opText:
		op.Text = cfgStr(m, "text", "")
		op.FontSize = num("font_size", 18)
		op.FontColor = col("font_color", opaqueBlack)
		op.PositionX, op.PositionY = integer("position_x", 50), integer("position_y", 50)
		op.HAlign = choice("horizontal_alignment", "center", "west", "center", "east", "left", "right")
		op.VAlign = choice("vertical_alignment", "middle", "north", "middle", "south", "top", "bottom", "center")
		op.LineLength = integer("line_length", 80)
		op.Font = strings.TrimSpace(cfgStr(m, "font", ""))
		if err == nil && (op.FontSize <= 0 || op.FontSize > 2000) {
			err = fmt.Errorf("font_size: must be between 0 and 2000")
		}
		if err == nil && op.LineLength < 1 {
			err = fmt.Errorf("line_length: must be at least 1")
		}
	case opTransparent:
		op.Color = col("color", opaqueRed)
		op.Fuzz = num("fuzz", 0)
		if err == nil && (op.Fuzz < 0 || op.Fuzz > 100) {
			err = fmt.Errorf("fuzz: must be between 0 and 100 (percent)")
		}
	default:
		return op, fmt.Errorf("operation: %q is not one of %s", name, strings.Join(editOperations, ", "))
	}
	return op, err
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// parseOperations turns the node config into the ordered list of pixel
// operations to run (one for a single operation, several for multi_step).
// information has no pixel operation and returns an empty list.
func parseOperations(config map[string]interface{}, operation string, mode hexAlphaMode) ([]editOp, error) {
	switch operation {
	case opInformation:
		return nil, nil
	case opMultiStep:
		steps, err := cfgSteps(config, "operations")
		if err != nil {
			return nil, err
		}
		ops := make([]editOp, 0, len(steps))
		for i, step := range steps {
			name := strings.TrimSpace(cfgStr(step, "operation", ""))
			if name == opInformation || name == opMultiStep {
				return nil, fmt.Errorf("operations[%d].operation: %q cannot be a step", i, name)
			}
			if name == opCreate && i > 0 {
				return nil, fmt.Errorf("operations[%d].operation: create can only be the first step", i)
			}
			op, err := parseOp(step, name, mode)
			if err != nil {
				return nil, fmt.Errorf("operations[%d].%w", i, err)
			}
			ops = append(ops, op)
		}
		return ops, nil
	default:
		op, err := parseOp(config, operation, mode)
		if err != nil {
			return nil, err
		}
		return []editOp{op}, nil
	}
}
