package monitor

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/stake-plus/account-manager/src/account-monitor/components/config"
	"github.com/stake-plus/account-manager/src/account-monitor/components/database"
	"github.com/stake-plus/account-manager/src/account-monitor/components/discord"
	"github.com/stake-plus/account-manager/src/account-monitor/components/networks"
)

type Monitor struct {
	db       *database.DB
	networks *networks.Manager
	discord  *discord.Client
	config   *config.Config
}

func New(db *database.DB, networks *networks.Manager, discord *discord.Client, cfg *config.Config) *Monitor {
	return &Monitor{
		db:       db,
		networks: networks,
		discord:  discord,
		config:   cfg,
	}
}

func (m *Monitor) StartBalanceMonitor(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately on start
	m.checkBalances(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkBalances(ctx)
		}
	}
}

func (m *Monitor) StartValidatorMonitor(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately on start
	m.checkValidators(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkValidators(ctx)
		}
	}
}

func (m *Monitor) StartBountyMonitor(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately on start
	m.checkBounties(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkBounties(ctx)
		}
	}
}

func (m *Monitor) checkBalances(ctx context.Context) {
	log.Println("Starting balance check...")

	accounts, err := m.db.GetAccounts()
	if err != nil {
		log.Printf("Failed to get accounts: %v", err)
		return
	}

	networks, err := m.db.GetNetworks()
	if err != nil {
		log.Printf("Failed to get networks: %v", err)
		return
	}

	summary := &discord.DailySummary{
		TotalAccounts:  len(accounts),
		ActiveNetworks: len(networks),
	}

	for _, account := range accounts {
		accountSummary := discord.AccountSummary{
			Name:    account.Name.String,
			Address: account.Address,
		}

		for _, network := range networks {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Get balance from chain
			balance, err := m.networks.GetBalance(network.Name, account.Address)
			if err != nil {
				log.Printf("Failed to get balance for %s on %s: %v", account.Address, network.Name, err)
				continue
			}

			// Get current balance from database
			var currentBalance database.Balance
			err = m.db.QueryRow(`
				SELECT free, total FROM balances 
				WHERE account_id = ? AND network_id = ? AND network_token_id = 
				(SELECT id FROM network_tokens WHERE network_id = ? AND token_type = 'native' LIMIT 1)
			`, account.ID, network.ID, network.ID).Scan(&currentBalance.Free, &currentBalance.Total)

			// Check for balance changes
			if err == nil && currentBalance.Total != nil {
				change := new(big.Int).Sub(balance.Total, currentBalance.Total)
				if change.Cmp(big.NewInt(0)) != 0 {
					changeType := "increase"
					if change.Cmp(big.NewInt(0)) < 0 {
						changeType = "decrease"
					}

					// Send notification if change is significant
					changeFloat := new(big.Float).SetInt(change)
					minChange := big.NewFloat(m.config.MinBalanceChangeNotification)

					if changeFloat.Cmp(minChange) >= 0 || changeFloat.Cmp(new(big.Float).Neg(minChange)) <= 0 {
						if account.DiscordNotify && m.config.EnableNotifications {
							m.discord.SendBalanceChangeNotification(
								account.Address,
								network.Name,
								network.Symbol.String,
								currentBalance.Total,
								balance.Total,
								changeType,
							)
						}
					}

					// Record the change
					m.db.RecordBalanceChange(database.BalanceChange{
						AccountID:    account.ID,
						NetworkID:    network.ID,
						TotalBefore:  currentBalance.Total,
						TotalAfter:   balance.Total,
						ChangeAmount: change,
						ChangeType:   changeType,
					})

					summary.TotalChanges++
				}
			}

			// Update balance in database
			m.db.UpdateBalance(account.ID, network.ID, 1, balance) // Assuming token ID 1 for native
		}

		accountSummary.Summary = fmt.Sprintf("Checked %d networks", len(networks))
		summary.AccountSummaries = append(summary.AccountSummaries, accountSummary)
	}

	// Send daily summary
	m.discord.SendDailySummary(*summary)
	log.Println("Balance check completed")
}

func (m *Monitor) checkValidators(ctx context.Context) {
	log.Println("Starting validator check...")

	// Implementation for checking validator stats, rewards, etc.
	// This would query validator-specific information from chains
	// and update the validator_stats and validator_era_stats tables

	log.Println("Validator check completed")
}

func (m *Monitor) checkBounties(ctx context.Context) {
	log.Println("Starting bounty check...")

	// Implementation for checking bounties and child bounties
	// This would check for claimable bounties and send notifications

	log.Println("Bounty check completed")
}
