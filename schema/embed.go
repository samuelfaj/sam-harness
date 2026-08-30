package schema

import _ "embed"

// ConfigJSON contains the public configuration schema.
//
//go:embed config.schema.json
var ConfigJSON []byte

// ReviewerOutputJSON contains the canonical reviewer response schema.
//
//go:embed reviewer-output.schema.json
var ReviewerOutputJSON []byte
