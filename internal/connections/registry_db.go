package connections

// connStrPlatform is a database platform authenticated by one connection
// string (the "connection_string" key the db.* nodes read).
func connStrPlatform(id, name, icon, example string) PlatformDef {
	return PlatformDef{
		ID: id, Name: name, Category: "database", ConnectVia: "API",
		Methods: []AuthMethod{MethodConnStr},
		Fields: map[AuthMethod][]CredentialField{MethodConnStr: {
			{Key: "connection_string", Label: "Connection String", Secret: true, Required: true, HelpText: "e.g. " + example},
		}},
		IconEmoji: icon,
	}
}

// fieldsPlatform is a platform whose credential is a set of named fields
// (host, user, password, TLS material …) merged into the node config.
func fieldsPlatform(id, name, category, icon string, fields ...CredentialField) PlatformDef {
	return PlatformDef{
		ID: id, Name: name, Category: category, ConnectVia: "API",
		Methods:   []AuthMethod{MethodAppPass},
		Fields:    map[AuthMethod][]CredentialField{MethodAppPass: fields},
		IconEmoji: icon,
	}
}

// tlsFields are the optional TLS keys the queue nodes share.
var tlsFields = []CredentialField{
	{Key: "ssl", Label: "Use TLS", HelpText: "true / false"},
	{Key: "ca", Label: "CA Certificate (PEM)", Secret: true},
	{Key: "cert", Label: "Client Certificate (PEM)", Secret: true},
	{Key: "key", Label: "Client Key (PEM)", Secret: true},
	{Key: "allow_unauthorized_certs", Label: "Allow Self-Signed Certificates", HelpText: "true / false"},
}

func withTLS(fields ...CredentialField) []CredentialField {
	return append(fields, tlsFields...)
}
