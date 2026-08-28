package schema

import _ "embed"

// ConfigJSON contains the public configuration schema.
//
//go:embed config.schema.json
var ConfigJSON []byte
