package workflow

// ConfigOperation is the operation a node's config names, read as the
// built-in nodes read it: its "operation" value when that is a string, else
// "" (a number, a bool, a list, an object or null names none, and a node
// that defaults a blank operation runs its default). The send gate reads it
// the same way (internal/hilsend), so it never judges an operation the node
// doesn't run.
func ConfigOperation(config map[string]interface{}) string {
	op, _ := config["operation"].(string)
	return op
}
