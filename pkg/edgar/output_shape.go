package edgar

import (
	"reflect"
	"strings"
)

type ShapedData struct {
	Data        any
	MetaUpdates map[string]any
}

func parseFields(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}

	seen := map[string]bool{}
	fields := []string{}
	for _, field := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(field)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		fields = append(fields, trimmed)
	}

	if len(fields) == 0 {
		return nil, NewCLIError(ErrorValidationRequired, "--fields requires at least one field")
	}

	return fields, nil
}

func shapeData(data any, fields []string, limit int) (ShapedData, error) {
	shaped, err := applyFields(data, fields)
	if err != nil {
		return ShapedData{}, err
	}

	meta := map[string]any{}
	if limit > 0 {
		value := reflect.ValueOf(shaped)
		if value.IsValid() && value.Kind() == reflect.Slice {
			totalCount := value.Len()
			if limit < totalCount {
				value = value.Slice(0, limit)
			}
			data = value.Interface()
			meta["total_count"] = totalCount
			meta["returned_count"] = value.Len()
			meta["truncated"] = value.Len() < totalCount
			return ShapedData{Data: data, MetaUpdates: meta}, nil
		}
	}

	return ShapedData{Data: shaped, MetaUpdates: meta}, nil
}

func applyFields(data any, fields []string) (any, error) {
	if len(fields) == 0 {
		return data, nil
	}

	value := reflect.ValueOf(data)
	if !value.IsValid() {
		return nil, NewCLIError(ErrorValidationRequired, "--fields can only be applied to object results or lists of objects")
	}

	if value.Kind() == reflect.Slice {
		out := make([]map[string]any, 0, value.Len())
		for idx := 0; idx < value.Len(); idx++ {
			projected, err := projectObject(value.Index(idx).Interface(), fields)
			if err != nil {
				return nil, err
			}
			out = append(out, projected)
		}
		return out, nil
	}

	return projectObject(data, fields)
}

func projectObject(source any, fields []string) (map[string]any, error) {
	sourceMap, err := objectToMap(source)
	if err != nil {
		return nil, NewCLIError(ErrorValidationRequired, "--fields can only be applied to object results or lists of objects")
	}

	projected := make(map[string]any, len(fields))
	for _, field := range fields {
		projected[field] = sourceMap[field]
	}
	return projected, nil
}

func objectToMap(source any) (map[string]any, error) {
	if source == nil {
		return nil, NewCLIError(ErrorValidationRequired, "--fields can only be applied to object results or lists of objects")
	}
	if sourceMap, ok := source.(map[string]any); ok {
		return sourceMap, nil
	}

	value := reflect.ValueOf(source)
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, NewCLIError(ErrorValidationRequired, "--fields can only be applied to object results or lists of objects")
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return nil, NewCLIError(ErrorValidationRequired, "--fields can only be applied to object results or lists of objects")
	}

	typ := value.Type()
	out := map[string]any{}
	for idx := 0; idx < value.NumField(); idx++ {
		field := typ.Field(idx)
		if field.PkgPath != "" {
			continue
		}
		name := field.Name
		if tag, ok := field.Tag.Lookup("json"); ok {
			tagName, _, _ := strings.Cut(tag, ",")
			if tagName == "-" {
				continue
			}
			if tagName != "" {
				name = tagName
			}
		}
		out[name] = value.Field(idx).Interface()
	}
	return out, nil
}
