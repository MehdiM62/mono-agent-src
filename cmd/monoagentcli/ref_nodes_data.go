package main

// Node reference entries (`monoagentcli ref node <type>`), one variable per
// category; ref.go joins them into nodeDocs.

// nodeDocsHttp is the "http" category of the node catalogue.
var nodeDocsHttp = []nodeDoc{
	{
		Type:     "http.request",
		Category: "http",
		Short:    "Make an HTTP request (GET, POST, PUT, DELETE, …)",
		Config: `{
  "method":  "POST",
  "url":     "https://api.example.com/data",
  "headers": { "Content-Type": "application/json" },
  "body":    "{{ json $json.payload }}"
}`,
		Inputs:  "any item (fields usable in config templates)",
		Outputs: "status_code, body (parsed), headers",
	},
	{
		Type:     "http.ftp",
		Category: "http",
		Short:    "Upload / download files via FTP",
		Config: `{
  "host":     "ftp.example.com",
  "port":     21,
  "username": "user",
  "password": "pass",
  "operation": "upload",
  "remote_path": "/pub/file.csv",
  "local_path":  "/tmp/file.csv"
}`,
		Inputs:  "none for download; local file path for upload",
		Outputs: "success, path, size_bytes",
	},
	{
		Type:     "http.ssh",
		Category: "http",
		Short:    "Execute commands on a remote host via SSH",
		Config: `{
  "host":     "server.example.com",
  "port":     22,
  "username": "admin",
  "password": "secret",
  "command":  "ls -la /var/log"
}`,
		Inputs:  "none",
		Outputs: "stdout, stderr, exit_code",
	},
}

// nodeDocsData is the "data" category of the node catalogue.
var nodeDocsData = []nodeDoc{
	{
		Type:     "data.spreadsheet",
		Category: "data",
		Short:    "Read / write CSV or Excel files",
		Config: `{
  "operation": "read",
  "file_path": "/tmp/data.csv",
  "has_header": true
}`,
		Inputs:  "file path or binary data",
		Outputs: "one item per row with column fields",
	},
	{
		Type:     "data.html",
		Category: "data",
		Short:    "Parse HTML — extract text, links, or specific elements",
		Config:   `{ "operation": "extract_text", "selector": "h1" }`,
		Inputs:   "html string field",
		Outputs:  "extracted text / elements",
	},
	{
		Type:     "data.xml",
		Category: "data",
		Short:    "Parse or generate XML",
		Config:   `{ "operation": "parse", "field": "raw_xml" }`,
		Inputs:   "xml string field",
		Outputs:  "parsed JSON structure",
	},
	{
		Type:     "data.markdown",
		Category: "data",
		Short:    "Convert Markdown to HTML or plain text",
		Config:   `{ "operation": "to_html", "field": "body" }`,
		Inputs:   "markdown string field",
		Outputs:  "html or text string",
	},
	{
		Type:     "data.crypto",
		Category: "data",
		Short:    "Hash, encode, or encrypt values",
		Config:   `{ "operation": "hash", "algorithm": "sha256", "field": "password" }`,
		Inputs:   "any item",
		Outputs:  "hashed / encoded value",
	},
	{
		Type:     "data.compression",
		Category: "data",
		Short:    "GZip or Zip compress / decompress data",
		Config:   `{ "operation": "compress", "format": "gzip" }`,
		Inputs:   "binary or string data",
		Outputs:  "compressed binary",
	},
	{
		Type:     "data.datetime",
		Category: "data",
		Short:    "Parse, format, or calculate date/time values",
		Config:   `{ "operation": "format", "field": "pubDate", "format": "2006-01-02" }`,
		Inputs:   "any item",
		Outputs:  "formatted date string or epoch int",
	},
	{
		Type:     "data.write_binary_file",
		Category: "data",
		Short:    "Write binary data (images, PDFs) to disk",
		Config:   `{ "file_path": "/tmp/{{ $json.filename }}" }`,
		Inputs:   "item with binary field",
		Outputs:  "path, size_bytes",
	},
}

// nodeDocsImage is the "image" category of the node catalogue.
var nodeDocsImage = []nodeDoc{
	{
		Type:     "image.info",
		Category: "image",
		Short:    "Read image metadata: width, height, format, size_bytes",
		Config:   `{ "field": "image_path" }`,
		Inputs:   "item with image path field",
		Outputs:  "width, height, format, size_bytes, image_path",
	},
	{
		Type:     "image.resize",
		Category: "image",
		Short:    "Resize an image (contain/cover/fill/width/height fit modes)",
		Config: `{
  "field":        "image_path",
  "width":        800,
  "height":       600,
  "fit":          "contain",
  "output_field": "resized_path"
}`,
		Inputs:  "item with image path field",
		Outputs: "output_field with path to resized image",
		Notes:   "fit values: contain (default), cover, fill, width (ignore height), height (ignore width). output_dir optional.",
	},
	{
		Type:     "image.crop",
		Category: "image",
		Short:    "Crop an image to a rectangle or aspect ratio",
		Config: `{
  "field":        "image_path",
  "x":            100,
  "y":            50,
  "width":        400,
  "height":       300,
  "output_field": "cropped_path"
}`,
		Inputs:  "item with image path field",
		Outputs: "output_field with path to cropped image",
		Notes:   "Use aspect_ratio (e.g. '16:9') instead of x/y/w/h for centre-crop. anchor: TopLeft, Top, TopRight, Left, Center (default), Right, BottomLeft, Bottom, BottomRight.",
	},
	{
		Type:     "image.thumbnail",
		Category: "image",
		Short:    "Create an exact-size thumbnail (fills and crops to fit)",
		Config: `{
  "field":        "image_path",
  "width":        200,
  "height":       200,
  "output_field": "thumb_path"
}`,
		Inputs:  "item with image path field",
		Outputs: "output_field with path to thumbnail",
		Notes:   "anchor controls crop position (default Center). output_dir optional.",
	},
	{
		Type:     "image.convert",
		Category: "image",
		Short:    "Convert image to a different format (jpeg/png/gif/tiff/bmp)",
		Config: `{
  "field":        "image_path",
  "format":       "jpeg",
  "quality":      85,
  "output_field": "converted_path"
}`,
		Inputs:  "item with image path field",
		Outputs: "output_field with path to converted image",
		Notes:   "quality applies to JPEG only (1-100, default 85). output_dir optional.",
	},
	{
		Type:     "image.remove_background",
		Category: "image",
		Short:    "Remove image background using U2-Net AI model (same model as rembg)",
		Config: `{
  "field":        "image_path",
  "output_field": "nobg_path",
  "bgcolor":      ""
}`,
		Inputs:  "item with image path field",
		Outputs: "output_field with path to PNG with transparent background",
		Notes: `No API key required — runs fully locally using ONNX Runtime.
On first use, automatically downloads:
  • ONNX Runtime 1.24.1 shared library     → ~/.monoagent/lib/
  • u2net.onnx (~176 MB)              → ~/.monoagent/models/
bgcolor: optional hex colour (e.g. "ffffff") to flatten alpha onto solid background.
Output is always PNG. Subsequent calls reuse cached model and ORT session.`,
	},
	{
		Type:     "image.adjust",
		Category: "image",
		Short:    "Adjust brightness, contrast, saturation, sharpness, blur, gamma, grayscale, sepia, invert",
		Config: `{
  "field":       "image_path",
  "brightness":  0.1,
  "contrast":    0.2,
  "saturation":  -0.1,
  "sharpen":     1.5,
  "blur":        0.0,
  "gamma":       1.0,
  "grayscale":   false,
  "sepia":       false,
  "invert":      false,
  "output_field": "adjusted_path"
}`,
		Inputs:  "item with image path field",
		Outputs: "output_field with path to adjusted image",
		Notes:   "brightness/contrast/saturation range -1.0 to 1.0 (0 = no change). sharpen = sigma (0 = off). blur = sigma (0 = off). gamma default 1.0.",
	},
	{
		Type:     "image.vault_save",
		Category: "image",
		Short:    "Save an item's image into the profile's image vault",
		Config: `{
  "field":        "image_path",
  "source":       "workflow",
  "output_field": "vault_id"
}`,
		Inputs:  "item with image path field",
		Outputs: "output_field with the new vault image id (e.g. img-001)",
		Notes:   "Copies the file into ~/.monoagent/vault/, independent of the source path. source is a free-text tag.",
	},
	{
		Type:     "image.vault_get",
		Category: "image",
		Short:    "Resolve an image vault id back into a local file path",
		Config: `{
  "field":        "vault_id",
  "output_field": "image_path"
}`,
		Inputs:  "item with a vault image id field (e.g. img-001, with or without a leading @)",
		Outputs: "output_field with the resolved local file path",
	},
}

// nodeDocsVault is the "vault" category of the node catalogue.
var nodeDocsVault = []nodeDoc{
	{
		Type:     "vault.secret_save",
		Category: "vault",
		Short:    "Encrypt item field values and save them into the credential vault",
		Config: `{
  "kind":         "secret",
  "name":         "my-api-key",
  "field_keys":   "[\"api_key\"]",
  "output_field": "vault_id"
}`,
		Inputs:  "item whose fields listed in field_keys hold the values to encrypt",
		Outputs: "output_field with the new vault entry id (e.g. sec-001)",
		Notes:   "kind: secret (arbitrary fields) or login (adds username/url). name must be unique per profile — a repeat name fails rather than overwriting. name is fixed for the whole node call, so a multi-item batch needs a per-item-unique name or it fails on the second item.",
	},
	{
		Type:     "vault.secret_get",
		Category: "vault",
		Short:    "Decrypt a vault entry and put its fields into the item stream",
		Config: `{
  "name":         "my-api-key",
  "output_field": "credential"
}`,
		Inputs:  "any item (name is a fixed config value, not read from the item)",
		Outputs: "output_field with the decrypted fields object",
		Notes:   "WARNING: puts plaintext into the item stream — persists unmasked in execution history and flows to every downstream node. Only the ~16 conventional key names in internal/workflow/redact.go (password, token, api_key, ...) get masked, and only at the MCP/REST (unless --full-outputs)/chat-tool display boundaries.",
	},
}

// nodeDocsDb is the "db" category of the node catalogue.
var nodeDocsDb = []nodeDoc{
	{
		Type:     "db.postgres",
		Category: "db",
		Short:    "Query or write to a PostgreSQL database",
		Config: `{
  "credential_id": "my-pg",
  "operation":     "query",
  "query":         "SELECT * FROM users WHERE active = true LIMIT 10"
}`,
		Inputs:  "none for SELECT; item fields usable as bind params",
		Outputs: "one item per row",
	},
	{
		Type:     "db.mysql",
		Category: "db",
		Short:    "Query or write to a MySQL / MariaDB database",
		Config:   `{ "credential_id": "my-mysql", "operation": "execute", "query": "INSERT INTO logs(msg) VALUES (?)", "params": ["{{ $json.message }}"] }`,
		Inputs:   "item fields as bind params",
		Outputs:  "affected_rows, last_insert_id",
	},
	{
		Type:     "db.mongodb",
		Category: "db",
		Short:    "Read or write documents in MongoDB",
		Config:   `{ "credential_id": "my-mongo", "database": "mydb", "collection": "events", "operation": "find", "filter": { "type": "news" } }`,
		Inputs:   "filter / document from config",
		Outputs:  "one item per document",
	},
	{
		Type:     "db.redis",
		Category: "db",
		Short:    "Get / set keys in Redis",
		Config:   `{ "credential_id": "my-redis", "operation": "set", "key": "cache:{{ $json.id }}", "value": "{{ json $json }}", "ttl": 3600 }`,
		Inputs:   "any item",
		Outputs:  "value (for get), ok (for set)",
	},
}
