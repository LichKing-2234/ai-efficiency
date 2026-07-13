package schema

import (
	"errors"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type QuotaResetRequestNodeApprover struct{ ent.Schema }

func (QuotaResetRequestNodeApprover) Fields() []ent.Field {
	return []ent.Field{
		field.Int("request_node_id").Immutable(),
		field.Int("user_id").Immutable(),
		field.String("display_name").Default("").Immutable(),
		field.String("email").Default("").Immutable(),
		field.Enum("source").Values("configured", "directory_representative").Immutable(),
		validatedJSONField(
			field.JSON("source_department_external_ids", []string{}).Default([]string{}).Immutable(),
			validateSourceDepartmentExternalIDs,
		),
		validatedJSONField(
			field.JSON("notification_ids", map[string]string{}).Default(map[string]string{}).Immutable(),
			validateNotificationIDs,
		),
		field.Time("created_at").Default(timeNow).Immutable(),
	}
}

func validateSourceDepartmentExternalIDs(externalIDs []string) error {
	if externalIDs == nil {
		return errors.New("source department external ids must not be nil")
	}
	return nil
}

func validateNotificationIDs(notificationIDs map[string]string) error {
	if notificationIDs == nil {
		return errors.New("notification ids must not be nil")
	}
	return nil
}

func (QuotaResetRequestNodeApprover) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("request_node_id", "user_id").Unique(),
		index.Fields("user_id", "request_node_id"),
	}
}
