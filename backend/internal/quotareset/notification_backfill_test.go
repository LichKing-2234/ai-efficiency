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
	if !row.Enabled || row.URL != robotURL || row.AuthType != quotaresetnotificationsetting.AuthTypeNone || row.CreatedByUserID != 7 || row.UpdatedByUserID != 8 {
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

func TestBackfillNotificationChannelsDisablesLegacyHTTPWeComAndPreservesGenericHTTP(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	legacyRobotURL := "http://qyapi.weixin.qq.com/cgi-bin/webhook/send" + "?" + "key=test-secret"
	legacyRobot := client.QuotaResetNotificationSetting.Create().
		SetEnabled(true).
		SetURL(legacyRobotURL).
		SetAuthType("none").
		SetCreatedByUserID(7).
		SetUpdatedByUserID(8).
		SaveX(ctx)
	genericHTTP := client.QuotaResetNotificationSetting.Create().
		SetEnabled(true).
		SetURL("http://hooks.example.com/quota-reset").
		SetAuthType("none").
		SetCreatedByUserID(9).
		SetUpdatedByUserID(10).
		SaveX(ctx)

	updated, err := BackfillNotificationChannelTypes(ctx, client)
	if err != nil {
		t.Fatalf("BackfillNotificationChannelTypes() error = %v", err)
	}
	if updated != 2 {
		t.Fatalf("updated = %d, want 2", updated)
	}
	legacyRobot = client.QuotaResetNotificationSetting.GetX(ctx, legacyRobot.ID)
	if legacyRobot.Enabled || !legacyRobot.ChannelTypeConfigured || legacyRobot.ChannelType != quotaresetnotificationsetting.ChannelTypeWecomGroupRobot || legacyRobot.URL != legacyRobotURL {
		t.Fatalf("legacy HTTP robot row = %+v, want disabled configured WeCom with preserved URL", legacyRobot)
	}
	genericHTTP = client.QuotaResetNotificationSetting.GetX(ctx, genericHTTP.ID)
	if !genericHTTP.Enabled || !genericHTTP.ChannelTypeConfigured || genericHTTP.ChannelType != quotaresetnotificationsetting.ChannelTypeGenericWebhook {
		t.Fatalf("generic HTTP row = %+v, want enabled configured generic webhook", genericHTTP)
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
