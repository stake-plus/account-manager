package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/stake-plus/account-manager/src/account-monitor/components/config"
	"github.com/stake-plus/account-manager/src/account-monitor/components/database"
	"github.com/stake-plus/account-manager/src/account-monitor/components/discord"
	"github.com/stake-plus/account-manager/src/account-monitor/components/monitor"
	"github.com/stake-plus/account-manager/src/account-monitor/components/networks"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize database
	db, err := database.Initialize(cfg.MySQLDSN)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Initialize Discord notifier
	discordClient := discord.NewClient(cfg.DiscordWebhook, cfg.DiscordChannelID)

	// Initialize network manager
	networkMgr, err := networks.NewManager(db, cfg)
	if err != nil {
		log.Fatalf("Failed to initialize network manager: %v", err)
	}

	// Initialize monitor
	mon := monitor.New(db, networkMgr, discordClient, cfg)

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down...")
		cancel()
	}()

	// Start monitoring loops
	go mon.StartBalanceMonitor(ctx, time.Duration(cfg.CheckIntervalHours)*time.Hour)
	go mon.StartValidatorMonitor(ctx, time.Duration(cfg.ValidatorCheckIntervalHours)*time.Hour)
	go mon.StartBountyMonitor(ctx, 30*time.Minute)

	// Initial network discovery
	log.Println("Starting initial network discovery...")
	if err := networkMgr.DiscoverNetworks(ctx); err != nil {
		log.Printf("Network discovery error: %v", err)
	}

	// Wait for shutdown
	<-ctx.Done()
	log.Println("Account monitor stopped")
}
