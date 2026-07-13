package schema

import (
	"errors"

	"entgo.io/ent"
)

func validatedQuotaResetJSONField[T any](jsonField ent.Field, validator func(T) error) ent.Field {
	descriptor := jsonField.Descriptor()
	descriptor.Validators = append(descriptor.Validators, validator)
	return jsonField
}

func newQuotaResetSlice[T any]() []T {
	return []T{}
}

func newQuotaResetMap[K comparable, V any]() map[K]V {
	return map[K]V{}
}

func validateQuotaResetSlice[T any](values []T) error {
	if values == nil {
		return errors.New("JSON snapshot container must not be nil")
	}
	return nil
}

func validateQuotaResetMap[K comparable, V any](values map[K]V) error {
	if values == nil {
		return errors.New("JSON snapshot container must not be nil")
	}
	return nil
}

func validateQuotaResetMapSlice(values []map[string]any) error {
	if err := validateQuotaResetSlice(values); err != nil {
		return err
	}
	for _, value := range values {
		if value == nil {
			return errors.New("JSON snapshot must not contain nil map elements")
		}
	}
	return nil
}
