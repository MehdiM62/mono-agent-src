package image

// ImageEditNodeSchema documents the config keys ImageEditNode reads — see
// internal/nodes/control.SetNodeSchema's doc comment for why this is a
// companion struct rather than the runtime config, and internal/tools/schemagen
// for the tag grammar. Fields shared by several operations (width, height,
// position_x, ...) carry no schema default because n8n's default differs per
// operation; the help text gives each one.
type ImageEditNodeSchema struct {
	Operation string `json:"operation" schema:"label=Operation,type=select,required,options=blur|border|composite|create|crop|draw|information|multi_step|resize|rotate|shear|text|transparent,default=border,help=What to do with the image. information adds format， size and color details to the item； multi_step runs a list of operations in order."`

	DataPropertyName string `json:"data_property_name" schema:"label=Input Binary Field,type=text,default=data,help=Item field holding the image as base64 (with file_name / mime_type next to it)， or a key of the item's binary data. Not needed for create."`

	Field string `json:"field" schema:"label=Image Path Field,type=text,help=Work on a file instead: item field holding a local image path. Leave empty to use the binary field； items that only carry image_path， path， file_path or media_path are read from that path."`

	Blur float64 `json:"blur" schema:"label=Blur Radius,type=number,default=5,min=0,max=1000,depends_on_key=operation,depends_on_values=blur,help=Accepted for n8n compatibility； the kernel size follows sigma."`

	Sigma float64 `json:"sigma" schema:"label=Sigma,type=number,default=2,min=0,max=1000,depends_on_key=operation,depends_on_values=blur,help=Gaussian blur strength in pixels. 0 leaves the image unchanged."`

	BorderWidth float64 `json:"border_width" schema:"label=Border Width,type=number,default=10,min=0,depends_on_key=operation,depends_on_values=border,help=Pixels added on the left and right."`

	BorderHeight float64 `json:"border_height" schema:"label=Border Height,type=number,default=10,min=0,depends_on_key=operation,depends_on_values=border,help=Pixels added on the top and bottom."`

	BorderColor string `json:"border_color" schema:"label=Border Color,type=text,default=#000000,depends_on_key=operation,depends_on_values=border,help=#rrggbb， #rrggbbaa， rgb()/rgba() or a color name."`

	DataPropertyNameComposite string `json:"data_property_name_composite" schema:"label=Overlay Image Field,type=text,depends_on_key=operation,depends_on_values=composite,help=Binary field of the image to lay over this one (in file-path mode: item field holding its path)."`

	Operator string `json:"operator" schema:"label=Operator,type=select,options=Over|In|Out|Atop|Xor|Plus|Add|Minus|Subtract|Multiply|Difference|Divide|Bumpmap|Copy|CopyOpacity|CopyRed|CopyGreen|CopyBlue|CopyBlack|CopyCyan|CopyMagenta|CopyYellow,default=Over,depends_on_key=operation,depends_on_values=composite,help=How overlay and image combine. The CMYK operators (CopyBlack， CopyCyan， CopyMagenta， CopyYellow) are rejected on RGB images."`

	PositionX float64 `json:"position_x" schema:"label=Position X,type=number,depends_on_key=operation,depends_on_values=composite|crop|text,help=Horizontal offset in pixels. Default 0 for composite and crop， 50 for text (measured from the alignment anchor)."`

	PositionY float64 `json:"position_y" schema:"label=Position Y,type=number,depends_on_key=operation,depends_on_values=composite|crop|text,help=Vertical offset in pixels. Default 0 for composite and crop， 50 for text."`

	BackgroundColor string `json:"background_color" schema:"label=Background Color,type=text,depends_on_key=operation,depends_on_values=create|rotate|shear,help=Canvas color for create (default white) or for areas a rotate or shear exposes (default transparent)."`

	Width float64 `json:"width" schema:"label=Width,type=number,depends_on_key=operation,depends_on_values=create|crop|resize,help=Pixels (percent for resize_option=percent). Default 50 for create， 500 for crop and resize."`

	Height float64 `json:"height" schema:"label=Height,type=number,depends_on_key=operation,depends_on_values=create|crop|resize,help=Pixels (percent for resize_option=percent). Default 50 for create， 500 for crop and resize."`

	ResizeOption string `json:"resize_option" schema:"label=Resize Option,type=select,options=maximumArea|minimumArea|ignoreAspectRatio|onlyIfLarger|onlyIfSmaller|percent,default=maximumArea,depends_on_key=operation,depends_on_values=resize,help=maximumArea: scale to an area of width×height pixels keeping aspect ratio； minimumArea: cover width and height； ignoreAspectRatio: exact size； onlyIfLarger: shrink to fit only； onlyIfSmaller: enlarge to fit only； percent: scale by percent."`

	Primitive string `json:"primitive" schema:"label=Primitive,type=select,options=rectangle|circle|line,default=rectangle,depends_on_key=operation,depends_on_values=draw,help=Filled rectangle， filled circle (start = centre， end = a point on the edge) or a one-pixel line."`

	Color string `json:"color" schema:"label=Color,type=text,default=#ff0000,depends_on_key=operation,depends_on_values=draw|transparent,help=Drawing color， or the color to make transparent."`

	StartPositionX float64 `json:"start_position_x" schema:"label=Start X,type=number,default=50,depends_on_key=operation,depends_on_values=draw"`

	StartPositionY float64 `json:"start_position_y" schema:"label=Start Y,type=number,default=50,depends_on_key=operation,depends_on_values=draw"`

	EndPositionX float64 `json:"end_position_x" schema:"label=End X,type=number,default=250,depends_on_key=operation,depends_on_values=draw"`

	EndPositionY float64 `json:"end_position_y" schema:"label=End Y,type=number,default=250,depends_on_key=operation,depends_on_values=draw"`

	CornerRadius float64 `json:"corner_radius" schema:"label=Corner Radius,type=number,default=0,min=0,depends_on_key=operation,depends_on_values=draw,help=Rounds the rectangle's corners."`

	Rotate float64 `json:"rotate" schema:"label=Degrees,type=number,default=0,min=-360,max=360,depends_on_key=operation,depends_on_values=rotate,help=Clockwise rotation； any angle. The canvas grows to fit."`

	DegreesX float64 `json:"degrees_x" schema:"label=Shear X (degrees),type=number,default=0,depends_on_key=operation,depends_on_values=shear"`

	DegreesY float64 `json:"degrees_y" schema:"label=Shear Y (degrees),type=number,default=0,depends_on_key=operation,depends_on_values=shear"`

	Text string `json:"text" schema:"label=Text,type=textarea,depends_on_key=operation,depends_on_values=text,help=Text to write. Long lines wrap at line_length characters."`

	FontSize float64 `json:"font_size" schema:"label=Font Size,type=number,default=18,min=1,depends_on_key=operation,depends_on_values=text"`

	FontColor string `json:"font_color" schema:"label=Font Color,type=text,default=#000000,depends_on_key=operation,depends_on_values=text"`

	HorizontalAlignment string `json:"horizontal_alignment" schema:"label=Horizontal Alignment,type=select,options=west|center|east,default=center,depends_on_key=operation,depends_on_values=text,help=west = left， east = right."`

	VerticalAlignment string `json:"vertical_alignment" schema:"label=Vertical Alignment,type=select,options=north|middle|south,default=middle,depends_on_key=operation,depends_on_values=text,help=north = top， south = bottom."`

	LineLength float64 `json:"line_length" schema:"label=Line Length,type=number,default=80,min=1,depends_on_key=operation,depends_on_values=text,help=Maximum characters per line before wrapping."`

	Fuzz float64 `json:"fuzz" schema:"label=Fuzz (%),type=number,default=0,min=0,max=100,depends_on_key=operation,depends_on_values=transparent,help=Also clear colors within this percent of the color."`

	Operations string `json:"operations" schema:"label=Operations,type=code,language=json,rows=8,depends_on_key=operation,depends_on_values=multi_step,placeholder=[{\"operation\": \"resize\"， \"width\": 800， \"height\": 600， \"resize_option\": \"onlyIfLarger\"}],help=JSON list of steps run in order； each has an operation and that operation's fields. create may only be the first step."`

	DestinationKey string `json:"destination_key" schema:"label=Output Binary Field,type=text,help=Binary field the result is written to. Defaults to the input binary field."`

	FileName string `json:"file_name" schema:"label=File Name,type=text,help=Name of the output file."`

	Format string `json:"format" schema:"label=Format,type=select,options=bmp|gif|jpeg|png|tiff|webp,help=Output format； defaults to the input format (png for create). webp is written as png (no pure-Go webp encoder)."`

	Quality float64 `json:"quality" schema:"label=Quality,type=number,min=0,max=100,help=JPEG quality 1-100 (default 95)."`

	Font string `json:"font" schema:"label=Font File,type=text,help=Path to a TrueType/OpenType font for text. Defaults to the built-in Go Regular font."`

	HexAlpha string `json:"hex_alpha" schema:"label=#rrggbbaa Alpha,type=select,options=alpha|opacity,default=alpha,help=alpha: the last hex byte is alpha (ff = opaque， CSS)； opacity: it is opacity as in n8n/GraphicsMagick (00 = opaque)."`

	OutputDir string `json:"output_dir" schema:"label=Output Directory,type=text,help=File-path mode: directory for the result (default: the source image's directory； required for create). ~ is expanded."`

	OutputField string `json:"output_field" schema:"label=Output Path Field,type=text,default=image_path,help=File-path mode: item field the result path is written to."`
}
