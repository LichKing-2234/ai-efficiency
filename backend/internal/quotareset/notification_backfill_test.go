package quotareset

import (
	"context"
	"testing"

	"github.com/ai-efficiency/backend/ent/quotaresetnotificationsetting"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestBackfillNotificationChannelsClassifiesExistingWeComOnce(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	robotURL := "https://qyapi.weixin.qq.com/cgi-bin/webhook/send" + "?" + "key=test-secret"
	row := client.QuotaResetNotificationSetting.Create().
		SetEnabled(true).
		SetURL(robotURL).
		SetAuthType("none").
		SetCreatedByUserID(7).
		SetUpdatedByUserID(8).
		SaveX(ctx)

	updated, err := BackfillNotificationChannelTypes(ctx, client)
	if err != nil {
		t.Fatalf("BackfillNotificationChannelTypes() error = %v", err)
	}
	if updated != 1 {
		t.Fatalf("updated = %d, want 1", updated)
	}
	row = client.QuotaResetNotificationSetting.GetX(ctx, row.ID)
	if !row.ChannelTypeConfigured || row.ChannelType != quotaresetnotificationsetting.ChannelTypeWecomGroupRobot {
		t.Fatalf("row channel = %q configured=%t, want WeCom configured", row.ChannelType, row.ChannelTypeConfigured)
	}
	if row.URL != robotURL || row.AuthType != quotaresetnotificationsetting.AuthTypeNone || row.CreatedByUserID != 7 || row.UpdatedByUserID != 8 {
		t.Fatalf("backfill changed preserved fields: %+v", row)
	}

	updated, err = BackfillNotificationChannelTypes(ctx, client)
	if err != nil {
		t.Fatalf("second BackfillNotificationChannelTypes() error = %v", err)
	}
	if updated != 0 {
		t.Fatalf("second updated = %d, want 0", updated)
	}
}

func TestBackfillNotificationChannelsKeepsExplicitGenericChoice(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	robotURL := "https://qyapi.weixin.qq.com/cgi-bin/webhook/send" + "?" + "key=test-secret"
	row := client.QuotaResetNotificationSetting.Create().
		SetChannelType(quotaresetnotificationsetting.ChannelTypeGenericWebhook).
		SetChannelTypeConfigured(true).
		SetEnabled(true).
		SetURL(robotURL).
		SetAuthType("none").
		SetCreatedByUserID(1).
		SetUpdatedByUserID(1).
		SaveX(ctx)

	updated, err := BackfillNotificationChannelTypes(ctx, client)
	if err != nil {
		t.Fatalf("BackfillNotificationChannelTypes() error = %v", err)
	}
	if updated != 0 {
		t.Fatalf("updated = %d, want 0", updated)
	}
	row = client.QuotaResetNotificationSetting.GetX(ctx, row.ID)
	if row.ChannelType != quotaresetnotificationsetting.ChannelTypeGenericWebhook || !row.ChannelTypeConfigured {
		t.Fatalf("row channel = %q configured=%t, want explicit generic choice", row.ChannelType, row.ChannelTypeConfigured)
	}
}
