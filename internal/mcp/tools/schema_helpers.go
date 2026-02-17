package tools

func schemaStringField(description string, examples ...interface{}) map[string]interface{} {
	return schemaField("string", description, examples...)
}

func schemaIntegerField(description string, examples ...interface{}) map[string]interface{} {
	return schemaField("integer", description, examples...)
}

func schemaNumberField(description string, examples ...interface{}) map[string]interface{} {
	return schemaField("number", description, examples...)
}

func schemaBooleanField(description string, examples ...interface{}) map[string]interface{} {
	return schemaField("boolean", description, examples...)
}

func schemaEnumStringField(description string, allowed []string, examples ...interface{}) map[string]interface{} {
	field := schemaField("string", description, examples...)
	field["enum"] = allowed
	return field
}

func schemaField(kind string, description string, examples ...interface{}) map[string]interface{} {
	field := map[string]interface{}{
		"type":        kind,
		"description": description,
	}
	if len(examples) > 0 {
		field["examples"] = examples
	}
	return field
}
