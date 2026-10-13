package data

// Companion schema structs (see crypto_schema.go and internal/tools/schemagen)
// for data.jwt, data.totp and data.debug_helper.

type JWTNodeSchema struct {
	Operation string `json:"operation" schema:"label=Operation,type=select,required,options=sign|decode|verify,default=sign"`

	KeyType string `json:"key_type" schema:"label=Key Type,type=select,options=passphrase|pem_key,default=passphrase,help=passphrase for HS* algorithms; pem_key for RS*， PS* and ES*.,depends_on_key=operation,depends_on_values=sign|verify"`

	Secret string `json:"secret" schema:"label=Secret,type=password,help=HMAC secret (key_type passphrase). Use an @secret: reference.,depends_on_key=key_type,depends_on_values=passphrase"`

	PrivateKey string `json:"private_key" schema:"label=Private Key (PEM),type=textarea,rows=4,help=PKCS#8， PKCS#1 or SEC 1 PEM key used to sign.,depends_on_key=operation,depends_on_values=sign"`

	PublicKey string `json:"public_key" schema:"label=Public Key (PEM),type=textarea,rows=4,help=PEM public key or certificate used to verify.,depends_on_key=operation,depends_on_values=verify"`

	Algorithm string `json:"algorithm" schema:"label=Algorithm,type=select,options=HS256|HS384|HS512|RS256|RS384|RS512|PS256|PS384|PS512|ES256|ES384|ES512,default=HS256,depends_on_key=operation,depends_on_values=sign|verify"`

	UseJSON bool `json:"use_json" schema:"label=Payload As JSON,type=boolean,default=false,help=Take the payload from claims_json instead of claims.,depends_on_key=operation,depends_on_values=sign"`

	Claims string `json:"claims" schema:"label=Payload Claims,type=code,language=json,placeholder={ \"subject\": \"user-1\"， \"expires_in\": 3600 },help=Object of claims. audience， issuer， jwtid and subject become aud， iss， jti and sub; expires_in and not_before (seconds from now) become exp and nbf; other keys are copied as-is. iat is added.,depends_on_key=operation,depends_on_values=sign"`

	ClaimsJSON string `json:"claims_json" schema:"label=Payload (JSON),type=code,language=json,help=Payload object used when Payload As JSON is on; may use {{ $json }} expressions (evaluated per item).,depends_on_key=operation,depends_on_values=sign"`

	HeaderClaims string `json:"header_claims" schema:"label=Header Claims (JSON),type=code,language=json,help=Extra header fields merged into the token header.,depends_on_key=operation,depends_on_values=sign"`

	Kid string `json:"kid" schema:"label=Key ID (kid),type=text,depends_on_key=operation,depends_on_values=sign"`

	Token string `json:"token" schema:"label=Token,type=text,placeholder={{ $json.token }},help=The JWT to decode or verify (evaluated per item).,depends_on_key=operation,depends_on_values=decode|verify"`

	Complete bool `json:"complete" schema:"label=Return Header And Signature,type=boolean,default=false,help=Output {header， payload， signature} instead of {payload}.,depends_on_key=operation,depends_on_values=decode|verify"`

	IgnoreExpiration bool `json:"ignore_expiration" schema:"label=Ignore Expiration,type=boolean,default=false,depends_on_key=operation,depends_on_values=verify"`

	IgnoreNotBefore bool `json:"ignore_not_before" schema:"label=Ignore Not Before,type=boolean,default=false,depends_on_key=operation,depends_on_values=verify"`

	ClockTolerance float64 `json:"clock_tolerance" schema:"label=Clock Tolerance (seconds),type=number,default=0,min=0,depends_on_key=operation,depends_on_values=verify"`
}

type TOTPNodeSchema struct {
	Operation string `json:"operation" schema:"label=Operation,type=select,options=generate_secret,default=generate_secret,help=Generates the current one-time code (n8n calls this operation Generate Secret)."`

	Secret string `json:"secret" schema:"label=Secret (Base32),type=password,required,help=The shared base32 secret. Use an @secret: reference."`

	Label string `json:"label" schema:"label=Label,type=text,placeholder=GitHub:octocat,help=Optional issuer:username."`

	Algorithm string `json:"algorithm" schema:"label=Algorithm,type=select,options=SHA1|SHA224|SHA256|SHA384|SHA512|SHA3-224|SHA3-256|SHA3-384|SHA3-512,default=SHA1"`

	Digits float64 `json:"digits" schema:"label=Digits,type=number,default=6,min=1,max=10"`

	Period float64 `json:"period" schema:"label=Period (seconds),type=number,default=30,min=1"`
}

type DebugHelperNodeSchema struct {
	Category string `json:"category" schema:"label=Category,type=select,required,options=do_nothing|throw_error|random_data,default=throw_error,help=do_nothing emits no items; throw_error fails the node; random_data generates fake items."`

	ThrowErrorType string `json:"throw_error_type" schema:"label=Error Type,type=select,options=NodeApiError|NodeOperationError|Error,default=NodeApiError,depends_on_key=category,depends_on_values=throw_error"`

	ThrowErrorMessage string `json:"throw_error_message" schema:"label=Error Message,type=text,default=Node has thrown an error,depends_on_key=category,depends_on_values=throw_error"`

	RandomDataType string `json:"random_data_type" schema:"label=Data Type,type=select,options=user|email|address|credit_card|uuid|mac_address|ipv4|ipv6|lat_long|semver|url|nanoid,default=user,depends_on_key=category,depends_on_values=random_data"`

	NanoidAlphabet string `json:"nanoid_alphabet" schema:"label=NanoId Alphabet,type=text,default=0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ,depends_on_key=random_data_type,depends_on_values=nanoid"`

	NanoidLength float64 `json:"nanoid_length" schema:"label=NanoId Length,type=number,default=16,min=1,max=1024,depends_on_key=random_data_type,depends_on_values=nanoid"`

	RandomDataSeed string `json:"random_data_seed" schema:"label=Seed,type=text,help=Same seed = same data. Empty = different every run.,depends_on_key=category,depends_on_values=random_data"`

	RandomDataCount float64 `json:"random_data_count" schema:"label=Number Of Items,type=number,default=10,min=0,max=100000,depends_on_key=category,depends_on_values=random_data"`

	RandomDataSingleArray bool `json:"random_data_single_array" schema:"label=Output As Single Array,type=boolean,default=false,help=Emit one item {generatedItems: [...]} instead of one item per record.,depends_on_key=category,depends_on_values=random_data"`
}
