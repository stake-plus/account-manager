package config

import (
	"os"
	"strconv"

	"github.com/stake-plus/account-manager/src/account-monitor/components/database"
)

type Config struct {
	MySQLDSN                     string
	DiscordToken                 string
	DiscordWebhook               string
	DiscordChannelID             string
	GuildID                      string
	AlertsChannelID              string
	SummaryChannelID             string
	MonitorRoleID                string
	CheckIntervalHours           int
	ValidatorCheckIntervalHours  int
	BountyCheckIntervalMinutes   int
	EnableNotifications          bool
	MinBalanceChangeNotification float64
	UseDiscordBot                bool
}

func Load() (*Config, error) {
	cfg := &Config{
		MySQLDSN:                     getEnvOrDefault("MYSQL_DSN", "root:password@tcp(127.0.0.1:3306)/account_monitor?parseTime=true"),
		DiscordToken:                 os.Getenv("DISCORD_TOKEN"),
		DiscordWebhook:               os.Getenv("DISCORD_WEBHOOK"),
		DiscordChannelID:             os.Getenv("DISCORD_CHANNEL_ID"),
		GuildID:                      os.Getenv("GUILD_ID"),
		AlertsChannelID:              os.Getenv("ALERTS_CHANNEL_ID"),
		SummaryChannelID:             os.Getenv("SUMMARY_CHANNEL_ID"),
		MonitorRoleID:                os.Getenv("MONITOR_ROLE_ID"),
		CheckIntervalHours:           24,
		ValidatorCheckIntervalHours:  8,
		BountyCheckIntervalMinutes:   30,
		EnableNotifications:          true,
		MinBalanceChangeNotification: 0.0001,
		UseDiscordBot:                false,
	}

	// Try to load settings from database first
	if db, err := database.Initialize(cfg.MySQLDSN); err == nil {
		defer db.Close()

		settings, err := database.LoadSettings(db)
		if err == nil && settings != nil {
			applyDatabaseSettings(cfg, settings)
		}
	}

	// Parse interval settings from environment
	if intervalStr := os.Getenv("CHECK_INTERVAL_HOURS"); intervalStr != "" {
		if val, err := strconv.Atoi(intervalStr); err == nil {
			cfg.CheckIntervalHours = val
		}
	}

	if intervalStr := os.Getenv("VALIDATOR_CHECK_INTERVAL_HOURS"); intervalStr != "" {
		if val, err := strconv.Atoi(intervalStr); err == nil {
			cfg.ValidatorCheckIntervalHours = val
		}
	}

	if intervalStr := os.Getenv("BOUNTY_CHECK_INTERVAL_MINUTES"); intervalStr != "" {
		if val, err := strconv.Atoi(intervalStr); err == nil {
			cfg.BountyCheckIntervalMinutes = val
		}
	}

	if enabledStr := os.Getenv("ENABLE_NOTIFICATIONS"); enabledStr != "" {
		cfg.EnableNotifications = enabledStr == "true" || enabledStr == "1"
	}

	if minChangeStr := os.Getenv("MIN_BALANCE_CHANGE"); minChangeStr != "" {
		if val, err := strconv.ParseFloat(minChangeStr, 64); err == nil {
			cfg.MinBalanceChangeNotification = val
		}
	}

	// Determine Discord mode after loading all settings
	if cfg.DiscordToken != "" && cfg.GuildID != "" {
		cfg.UseDiscordBot = true
	} else if cfg.DiscordWebhook == "" && cfg.DiscordToken == "" {
		// If no webhook and no bot token, notifications disabled
		cfg.EnableNotifications = false
	}

	return cfg, nil
}

func applyDatabaseSettings(cfg *Config, settings map[string]string) {
	if token, ok := settings["discord_token"]; ok && token != "" && cfg.DiscordToken == "" {
		cfg.DiscordToken = token
	}
	if webhook, ok := settings["discord_webhook"]; ok && webhook != "" && cfg.DiscordWebhook == "" {
		cfg.DiscordWebhook = webhook
	}
	if channelID, ok := settings["discord_channel_id"]; ok && channelID != "" && cfg.DiscordChannelID == "" {
		cfg.DiscordChannelID = channelID
	}
	if guildID, ok := settings["guild_id"]; ok && guildID != "" && cfg.GuildID == "" {
		cfg.GuildID = guildID
	}
	if alertsID, ok := settings["alerts_channel_id"]; ok && alertsID != "" && cfg.AlertsChannelID == "" {
		cfg.AlertsChannelID = alertsID
	}
	if summaryID, ok := settings["summary_channel_id"]; ok && summaryID != "" && cfg.SummaryChannelID == "" {
		cfg.SummaryChannelID = summaryID
	}
	if roleID, ok := settings["monitor_role_id"]; ok && roleID != "" && cfg.MonitorRoleID == "" {
		cfg.MonitorRoleID = roleID
	}
	if interval, ok := settings["check_interval_hours"]; ok && interval != "" {
		if val, err := strconv.Atoi(interval); err == nil {
			cfg.CheckIntervalHours = val
		}
	}
	if interval, ok := settings["validator_check_interval_hours"]; ok && interval != "" {
		if val, err := strconv.Atoi(interval); err == nil {
			cfg.ValidatorCheckIntervalHours = val
		}
	}
	if interval, ok := settings["bounty_check_interval_minutes"]; ok && interval != "" {
		if val, err := strconv.Atoi(interval); err == nil {
			cfg.BountyCheckIntervalMinutes = val
		}
	}
	if enabled, ok := settings["enable_notifications"]; ok && enabled != "" {
		cfg.EnableNotifications = enabled == "true" || enabled == "1"
	}
	if minChange, ok := settings["min_balance_change_notification"]; ok && minChange != "" {
		if val, err := strconv.ParseFloat(minChange, 64); err == nil {
			cfg.MinBalanceChangeNotification = val
		}
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
