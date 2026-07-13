package schema

import (
	"errors"

	"entgo.io/ent"
	entsql "entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type QuotaResetRequestNode struct{ ent.Schema }

func (QuotaResetRequestNode) Fields() []ent.Field {
	return []ent.Field{
		field.Int("request_id").Immutable(),
		field.Int("position").NonNegative().Immutable(),
		field.Enum("node_type").Values("requester_departments", "configured_department").Immutable(),
		field.String("label").Default("").Immutable(),
		validatedJSONField(
			field.JSON("department_snapshots", []map[string]any{}).Default([]map[string]any{}).Immutable(),
			validateDepartmentSnapshots,
		),
		field.Enum("status").Values("queued", "active", "approved", "satisfied_by_prior_approval", "skipped_no_approver", "rejected").Default("queued"),
		field.Bool("admin_fallback_required").Default(false).Immutable(),
		field.Int("satisfied_by_decision_id").Optional().Nillable(),
		field.Time("activated_at").Optional().Nillable(),
		field.Time("completed_at").Optional().Nillable(),
		field.Time("created_at").Default(timeNow).Immutable(),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func validatedJSONField[T any](jsonField ent.Field, validator func(T) error) ent.Field {
	descriptor := jsonField.Descriptor()
	descriptor.Validators = append(descriptor.Validators, validator)
	return jsonField
}

func validateDepartmentSnapshots(snapshots []map[string]any) error {
	if snapshots == nil {
		return errors.New("department snapshots must not be nil")
	}
	for _, snapshot := range snapshots {
		if snapshot == nil {
			return errors.New("department snapshots must not contain nil elements")
		}
	}
	return nil
}

func (QuotaResetRequestNode) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("request_id", "position").Unique(),
		index.Fields("request_id").
			Unique().
			Annotations(entsql.IndexWhere("status = 'active'")),
		index.Fields("request_id", "status"),
		index.Fields("status", "activated_at"),
	}
}
