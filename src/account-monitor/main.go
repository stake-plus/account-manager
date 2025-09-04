package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/stake-plus/account-manager/src/account-monitor/components/config"
	"github.com/stake-plus/account-manager/src/account-monitor/components/database"
	"github.com/stake-plus/account-manager/src/account-monitor/components/discord"
	monitor "github.com/stake-plus/account-manager/src/account-monitor/components/monitor"
	"github.com/stake-plus/account-manager/src/account-monitor/components/networks"
)

func main() {
	log.Println("Account Monitor starting...")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Validate configuration
	if cfg.MySQLDSN == "" {
		log.Fatal("MySQL DSN is required")
	}

	if !cfg.EnableNotifications {
		log.Println("WARNING: Notifications are disabled")
	} else if cfg.DiscordWebhook == "" && !cfg.UseDiscordBot {
		log.Println("WARNING: No Discord configuration found, notifications will be limited")
	}

	// Initialize database
	db, err := database.Initialize(cfg.MySQLDSN)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("Error closing database: %v", err)
		}
	}()

	// Initialize Discord client
	var discordClient *discord.Client
	if cfg.EnableNotifications {
		if cfg.UseDiscordBot {
			// Check if bot has proper permissions
			if cfg.DiscordToken != "" {
				// Extract bot ID from token (first part before first dot)
				botID := cfg.DiscordToken
				if dotIndex := len(cfg.DiscordToken); dotIndex > 18 {
					botID = cfg.DiscordToken[:18]
				}

				// Generate invite URL with proper permissions
				permissions := "2147485696" // Send Messages, Read Messages, Embed Links
				inviteURL := fmt.Sprintf("https://discord.com/api/oauth2/authorize?client_id=%s&permissions=%s&scope=bot",
					botID, permissions)

				log.Printf("Make sure the bot is invited with proper permissions: %s", inviteURL)
			}

			discordClient, err = discord.NewBotClient(cfg.DiscordToken, cfg.AlertsChannelID, cfg.SummaryChannelID)
			if err != nil {
				log.Printf("Failed to initialize Discord bot client: %v", err)
				// Fall back to webhook if available
				if cfg.DiscordWebhook != "" {
					log.Println("Falling back to webhook client")
					discordClient = discord.NewWebhookClient(cfg.DiscordWebhook, cfg.DiscordChannelID)
				} else {
					log.Println("Discord notifications disabled due to initialization failure")
					cfg.EnableNotifications = false
				}
			} else {
				log.Printf("Discord bot connected successfully")
				log.Printf("Alerts will be sent to channel: %s", cfg.AlertsChannelID)
				log.Printf("Summaries will be sent to channel: %s", cfg.SummaryChannelID)
			}
		} else if cfg.DiscordWebhook != "" {
			discordClient = discord.NewWebhookClient(cfg.DiscordWebhook, cfg.DiscordChannelID)
		}
	}

	// Initialize network manager
	log.Println("Initializing network manager...")
	networkMgr, err := networks.NewManager(db, cfg)
	if err != nil {
		log.Fatalf("Failed to initialize network manager: %v", err)
	}

	// Initialize monitor
	log.Println("Initializing monitor...")
	mon := monitor.New(db, networkMgr, discordClient, cfg)

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Printf("Received signal: %v", sig)
		log.Println("Starting graceful shutdown...")
		cancel()
	}()

	// Initial network discovery
	log.Println("Starting initial network discovery...")
	if err := networkMgr.DiscoverNetworks(ctx); err != nil {
		if err == context.Canceled {
			log.Println("Network discovery canceled")
		} else {
			log.Printf("Network discovery error: %v", err)
		}
	}

	// Start monitoring loops
	log.Println("Starting monitoring services...")

	// Balance monitor
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Balance monitor panic recovered: %v", r)
			}
		}()
		mon.StartBalanceMonitor(ctx, time.Duration(cfg.CheckIntervalHours)*time.Hour)
	}()

	// Validator monitor
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Validator monitor panic recovered: %v", r)
			}
		}()
		mon.StartValidatorMonitor(ctx, time.Duration(cfg.ValidatorCheckIntervalHours)*time.Hour)
	}()

	// Bounty monitor
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Bounty monitor panic recovered: %v", r)
			}
		}()
		mon.StartBountyMonitor(ctx, time.Duration(cfg.BountyCheckIntervalMinutes)*time.Minute)
	}()

	// Network refresh loop
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Network refresh panic recovered: %v", r)
			}
		}()

		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				log.Println("Refreshing network information...")
				if err := networkMgr.DiscoverNetworks(ctx); err != nil {
					if err != context.Canceled {
						log.Printf("Network refresh error: %v", err)
					}
				}
			}
		}
	}()

	log.Println("Account monitor is running. Press Ctrl+C to stop.")

	// Wait for shutdown
	<-ctx.Done()

	// Give goroutines time to cleanup
	log.Println("Waiting for services to stop...")
	time.Sleep(2 * time.Second)

	log.Println("Account monitor stopped")
}
