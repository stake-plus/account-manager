package config

import (
	"os"
	"strconv"

	"github.com/stake-plus/account-manager/src/account-monitor/components/database"
)

type Config struct {
	MySQLDSN                     string
	DiscordWebhook               string
	DiscordChannelID             string
	CheckIntervalHours           int
	ValidatorCheckIntervalHours  int
	EnableNotifications          bool
	MinBalanceChangeNotification float64
}

func Load() (*Config, error) {
	cfg := &Config{
		MySQLDSN:                     getEnvOrDefault("MYSQL_DSN", "root:password@tcp(127.0.0.1:3306)/account_monitor"),
		DiscordWebhook:               os.Getenv("DISCORD_WEBHOOK"),
		DiscordChannelID:             os.Getenv("DISCORD_CHANNEL_ID"),
		CheckIntervalHours:           24,
		ValidatorCheckIntervalHours:  8,
		EnableNotifications:          true,
		MinBalanceChangeNotification: 0.0001,
	}

	// Try to load settings from database if connection is available
	if db, err := database.Initialize(cfg.MySQLDSN); err == nil {
		defer db.Close()
		settings, _ := database.LoadSettings(db)

		if webhook, ok := settings["discord_webhook_url"]; ok && cfg.DiscordWebhook == "" {
			cfg.DiscordWebhook = webhook
		}
		if channelID, ok := settings["discord_channel_id"]; ok && cfg.DiscordChannelID == "" {
			cfg.DiscordChannelID = channelID
		}
		if interval, ok := settings["check_interval_hours"]; ok {
			if val, err := strconv.Atoi(interval); err == nil {
				cfg.CheckIntervalHours = val
			}
		}
		if interval, ok := settings["validator_check_interval_hours"]; ok {
			if val, err := strconv.Atoi(interval); err == nil {
				cfg.ValidatorCheckIntervalHours = val
			}
		}
		if enabled, ok := settings["enable_notifications"]; ok {
			cfg.EnableNotifications = enabled == "true"
		}
		if minChange, ok := settings["min_balance_change_notification"]; ok {
			if val, err := strconv.ParseFloat(minChange, 64); err == nil {
				cfg.MinBalanceChangeNotification = val
			}
		}
	}

	return cfg, nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
