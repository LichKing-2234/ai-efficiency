package quotareset

import (
	"context"
	"testing"

	entcredential "github.com/ai-efficiency/backend/ent/credential"
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

func TestBackfillNotificationChannelsNormalizesLegacyWeComBearerAuth(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	credential := client.Credential.Create().
		SetName("Legacy WeCom bearer token").
		SetKind(entcredential.KindSecretText).
		SetPayload("synthetic-encrypted-payload").
		SaveX(ctx)
	robotURL := "https://qyapi.weixin.qq.com/cgi-bin/webhook/send" + "?" + "key=test-secret"
	row := client.QuotaResetNotificationSetting.Create().
		SetEnabled(true).
		SetURL(robotURL).
		SetAuthType(quotaresetnotificationsetting.AuthTypeBearerToken).
		SetCredentialID(credential.ID).
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
	if !row.Enabled || !row.ChannelTypeConfigured || row.ChannelType != quotaresetnotificationsetting.ChannelTypeWecomGroupRobot {
		t.Fatalf("migrated WeCom row = %+v, want enabled configured WeCom", row)
	}
	if row.AuthType != quotaresetnotificationsetting.AuthTypeNone || row.CredentialID != nil {
		t.Fatalf("migrated WeCom auth = %s/%v, want none/nil", row.AuthType, row.CredentialID)
	}
	if err := validateNotificationSettings(ctx, client, row.Enabled, row.ChannelType, row.URL, row.AuthType, row.CredentialID); err != nil {
		t.Fatalf("validateNotificationSettings(migrated WeCom) error = %v", err)
	}
	settings, err := NewService(client, nil, nil, nil).GetNotificationSettings(ctx)
	if err != nil {
		t.Fatalf("GetNotificationSettings() error = %v", err)
	}
	if settings.AuthType != quotaresetnotificationsetting.AuthTypeNone.String() || settings.CredentialID != nil {
		t.Fatalf("settings response auth = %s/%v, want none/nil", settings.AuthType, settings.CredentialID)
	}
}

func TestBackfillNotificationChannelsDisablesLegacyHTTPWeComAndPreservesGenericHTTP(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	credential := client.Credential.Create().
		SetName("Generic webhook bearer token").
		SetKind(entcredential.KindSecretText).
		SetPayload("synthetic-encrypted-payload").
		SaveX(ctx)
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
		SetAuthType(quotaresetnotificationsetting.AuthTypeBearerToken).
		SetCredentialID(credential.ID).
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
	if !genericHTTP.Enabled || !genericHTTP.ChannelTypeConfigured || genericHTTP.ChannelType != quotaresetnotificationsetting.ChannelTypeGenericWebhook || genericHTTP.AuthType != quotaresetnotificationsetting.AuthTypeBearerToken || genericHTTP.CredentialID == nil || *genericHTTP.CredentialID != credential.ID {
		t.Fatalf("generic HTTP row = %+v, want enabled configured generic webhook with preserved bearer auth", genericHTTP)
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
