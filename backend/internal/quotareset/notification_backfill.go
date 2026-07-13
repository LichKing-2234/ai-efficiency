package quotareset

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/quotaresetnotificationsetting"
)

func BackfillNotificationChannelTypes(ctx context.Context, client *ent.Client) (int, error) {
	rows, err := client.QuotaResetNotificationSetting.Query().
		Where(quotaresetnotificationsetting.ChannelTypeConfigured(false)).
		All(ctx)
	if err != nil {
		return 0, fmt.Errorf("list notification settings for channel backfill: %w", err)
	}

	updated := 0
	for _, row := range rows {
		channelType := quotaresetnotificationsetting.ChannelTypeGenericWebhook
		disable := false
		parsed, _ := url.Parse(strings.TrimSpace(row.URL))
		if parsed != nil && strings.EqualFold(parsed.Hostname(), "qyapi.weixin.qq.com") && parsed.Path == "/cgi-bin/webhook/send" {
			channelType = quotaresetnotificationsetting.ChannelTypeWecomGroupRobot
			disable = !strings.EqualFold(parsed.Scheme, "https")
		}
		update := client.QuotaResetNotificationSetting.Update().
			Where(
				quotaresetnotificationsetting.IDEQ(row.ID),
				quotaresetnotificationsetting.ChannelTypeConfigured(false),
			).
			SetChannelType(channelType).
			SetChannelTypeConfigured(true)
		if disable {
			update.SetEnabled(false)
		}
		affected, err := update.Save(ctx)
		if err != nil {
			return updated, fmt.Errorf("backfill notification setting %d: %w", row.ID, err)
		}
		updated += affected
	}
	return updated, nil
}
