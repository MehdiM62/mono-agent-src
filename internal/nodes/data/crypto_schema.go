package data

// CryptoNodeSchema documents the config keys CryptoNode.Execute reads out of
// its map[string]interface{} config — see
// internal/nodes/control.SetNodeSchema's doc comment for why this is a
// companion struct rather than the runtime config, and
// internal/tools/schemagen for the tag grammar.
//
// The hand-written schemas/data.crypto.json this replaces was missing three
// operations Execute has always supported ("uuid", "random_bytes", plus the
// "output_field"/"encoding"/"length" fields those and the hash/hmac
// operations read) — CryptoNode.Execute never exposed a way to generate a
// UUID or random bytes, or to pick base64 vs hex output encoding, through
// the UI. Generating from the struct surfaces that gap; it's fixed here by
// adding those fields rather than silently carrying the gap forward.
//
// Execute also accepts a "key" config alias for "secret" (checked only when
// "secret" is empty) — that's an undocumented alias for the same field, not
// a distinct config value, so it isn't given its own schema entry.
type CryptoNodeSchema struct {
	Operation string `json:"operation" schema:"label=Operation,type=select,required,options=hash_md5|hash_sha256|hash_sha512|hmac_sha256|uuid|random_bytes|base64_encode|base64_decode|hash|hmac|sign|generate|encrypt|decrypt,default=hash_sha256,help=hash/hmac/sign/generate/encrypt/decrypt are the n8n Crypto actions: they read value and write data_property_name."`

	Field string `json:"field" schema:"label=Input Field,type=text,placeholder=password,help=Item field to read input from. Not used for uuid/random_bytes.,depends_on_key=operation,depends_on_values=hash_md5|hash_sha256|hash_sha512|hmac_sha256|base64_encode|base64_decode"`

	Value string `json:"value" schema:"label=Value,type=text,placeholder={{ $json.password }},help=Value to process (evaluated per item).,depends_on_key=operation,depends_on_values=hash|hmac|sign|encrypt|decrypt"`

	Type string `json:"type" schema:"label=Hash Type,type=select,options=MD5|SHA1|SHA224|SHA256|SHA384|SHA512|SHA3-224|SHA3-256|SHA3-384|SHA3-512|RIPEMD160,default=SHA256,depends_on_key=operation,depends_on_values=hash|hmac"`

	BinaryData bool `json:"binary_data" schema:"label=Hash Binary Data,type=boolean,default=false,help=Hash a binary property instead of value.,depends_on_key=operation,depends_on_values=hash|hmac"`

	BinaryPropertyName string `json:"binary_property_name" schema:"label=Binary Property,type=text,default=data,depends_on_key=binary_data,depends_on_values=true"`

	Secret string `json:"secret" schema:"label=HMAC Secret,type=password,depends_on_key=operation,depends_on_values=hmac_sha256|hmac"`

	Algorithm string `json:"algorithm" schema:"label=Signing Algorithm,type=select,options=RSA-SHA256|RSA-SHA384|RSA-SHA512|RSA-SHA1|RSA-SHA224|RSA-RIPEMD160|sha256|sha384|sha512,default=RSA-SHA256,help=Digest used with the private key (RSA PKCS#1 v1.5 or ECDSA; Ed25519 keys ignore it).,depends_on_key=operation,depends_on_values=sign"`

	PrivateKey string `json:"private_key" schema:"label=Private Key (PEM),type=textarea,rows=4,help=Key for sign， and for asymmetric decrypt (RSA).,depends_on_key=operation,depends_on_values=sign|decrypt"`

	PublicKey string `json:"public_key" schema:"label=Public Key (PEM),type=textarea,rows=4,help=RSA key for asymmetric encrypt.,depends_on_key=operation,depends_on_values=encrypt"`

	Mode string `json:"mode" schema:"label=Mode,type=select,options=symmetric|asymmetric,default=symmetric,help=symmetric: passphrase + cipher (n8n-compatible payload). asymmetric: RSA-OAEP.,depends_on_key=operation,depends_on_values=encrypt|decrypt"`

	Passphrase string `json:"passphrase" schema:"label=Passphrase,type=password,depends_on_key=mode,depends_on_values=symmetric"`

	Cipher string `json:"cipher" schema:"label=Cipher,type=select,options=aes-256-gcm|aes-192-gcm|aes-128-gcm|chacha20-poly1305,default=aes-256-gcm,depends_on_key=mode,depends_on_values=symmetric"`

	OAEPHash string `json:"oaep_hash" schema:"label=OAEP Hash,type=select,options=sha256|sha1,default=sha256,depends_on_key=mode,depends_on_values=asymmetric"`

	EncodingType string `json:"encoding_type" schema:"label=Generate Type,type=select,options=uuid|ascii|base64|hex,default=uuid,depends_on_key=operation,depends_on_values=generate"`

	StringLength float64 `json:"string_length" schema:"label=String Length,type=number,default=32,min=1,depends_on_key=encoding_type,depends_on_values=ascii|base64|hex"`

	DataPropertyName string `json:"data_property_name" schema:"label=Property Name,type=text,default=data,help=Field the result is written to (dot paths allowed).,depends_on_key=operation,depends_on_values=hash|hmac|sign|generate|encrypt|decrypt"`

	OutputField string `json:"output_field" schema:"label=Output Field,type=text,help=Defaults to the input field， overwriting it."`

	Encoding string `json:"encoding" schema:"label=Output Encoding,type=select,options=hex|base64,default=hex,help=Encoding used for hash/hmac/random_bytes output.,depends_on_key=operation,depends_on_values=hash_md5|hash_sha256|hash_sha512|hmac_sha256|random_bytes|hash|hmac|sign"`

	Length float64 `json:"length" schema:"label=Random Byte Count,type=number,default=16,depends_on_key=operation,depends_on_values=random_bytes"`
}
