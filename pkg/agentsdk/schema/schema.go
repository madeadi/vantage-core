package schema

func EnumSchema[T ~string](values ...T) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}
