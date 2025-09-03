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
	types "github.com/stake-plus/account-manager/src/account-monitor/components/types"
)

type Monitor struct {
	db       *database.DB
	networks *networks.Manager
	discord  *discord.Client
	config   *config.Config
}

type AccountBalance struct {
	Account       types.Account
	Balances      map[string]*types.Balance // network -> balance
	TotalValue    *big.Int                  // Total value across all networks
	ChangeValue   *big.Int                  // Change from yesterday
	ChangePercent float64                   // Percentage change
}

func New(db *database.DB, networks *networks.Manager, discord *discord.Client, config *config.Config) *Monitor {
	return &Monitor{
		db:       db,
		networks: networks,
		discord:  discord,
		config:   config,
	}
}

func (m *Monitor) StartBalanceMonitor(ctx context.Context, interval time.Duration) {
	// Run immediately
	m.checkBalances(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkBalances(ctx)
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

	// Track all balances for daily summary
	accountBalances := make(map[uint]*AccountBalance)
	totalPortfolioValue := big.NewInt(0)
	totalDailyRevenue := big.NewInt(0)

	for _, account := range accounts {
		if !account.MonitorEnabled {
			continue
		}

		accountBalance := &AccountBalance{
			Account:     account,
			Balances:    make(map[string]*types.Balance),
			TotalValue:  big.NewInt(0),
			ChangeValue: big.NewInt(0),
		}

		for _, network := range networks {
			if !network.Active {
				continue
			}

			// Get current balance
			balance, err := m.networks.GetBalance(network.Name, account.Address)
			if err != nil {
				log.Printf("Failed to get balance for %s on %s: %v", account.Address, network.Name, err)
				continue
			}

			// Store in account balances
			accountBalance.Balances[network.Name] = &balance

			// Add to total value
			accountBalance.TotalValue.Add(accountBalance.TotalValue, balance.Total)

			// Get native token for this network
			var nativeToken types.NetworkToken
			err = m.db.QueryRow(`
				SELECT id, symbol, decimals FROM network_tokens 
				WHERE network_id = ? AND token_type = 'native'
			`, network.ID).Scan(&nativeToken.ID, &nativeToken.Symbol, &nativeToken.Decimals)

			if err != nil {
				log.Printf("Failed to get native token for network %s: %v", network.Name, err)
				continue
			}

			// Check for balance changes
			var previousBalance types.Balance
			err = m.db.QueryRow(`
				SELECT free, reserved, misc_frozen, fee_frozen, bonded, total 
				FROM balances 
				WHERE account_id = ? AND network_id = ? AND network_token_id = ?
			`, account.ID, network.ID, nativeToken.ID).Scan(
				&previousBalance.Free, &previousBalance.Reserved,
				&previousBalance.MiscFrozen, &previousBalance.FeeFrozen,
				&previousBalance.Bonded, &previousBalance.Total,
			)

			var balanceID uint
			balanceExists := err == nil

			// Calculate change
			change := new(big.Int).Sub(balance.Total, previousBalance.Total)

			// Track change for this account
			accountBalance.ChangeValue.Add(accountBalance.ChangeValue, change)

			// Update or insert balance
			if balanceExists {
				// Update existing balance
				_, err = m.db.Exec(`
					UPDATE balances SET 
						free = ?, reserved = ?, misc_frozen = ?, 
						fee_frozen = ?, bonded = ?, total = ?, 
						last_updated = NOW()
					WHERE account_id = ? AND network_id = ? AND network_token_id = ?
				`, balance.Free.String(), balance.Reserved.String(),
					balance.MiscFrozen.String(), balance.FeeFrozen.String(),
					balance.Bonded.String(), balance.Total.String(),
					account.ID, network.ID, nativeToken.ID)

				// Get balance ID for history
				m.db.QueryRow(`
					SELECT id FROM balances 
					WHERE account_id = ? AND network_id = ? AND network_token_id = ?
				`, account.ID, network.ID, nativeToken.ID).Scan(&balanceID)

			} else {
				// Insert new balance
				result, err := m.db.Exec(`
					INSERT INTO balances 
					(account_id, network_id, network_token_id, free, reserved, 
					 misc_frozen, fee_frozen, bonded, total)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
				`, account.ID, network.ID, nativeToken.ID,
					balance.Free.String(), balance.Reserved.String(),
					balance.MiscFrozen.String(), balance.FeeFrozen.String(),
					balance.Bonded.String(), balance.Total.String())

				if err == nil {
					id, _ := result.LastInsertId()
					balanceID = uint(id)
				}
			}

			// Record balance change history if there was a change
			if change.Cmp(big.NewInt(0)) != 0 {
				changeType := "increase"
				if change.Cmp(big.NewInt(0)) < 0 {
					changeType = "decrease"
				}

				// Check if change is significant enough to notify
				changeFloat := new(big.Float).SetInt(change)
				divisor := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(nativeToken.Decimals)), nil))
				changeFloat.Quo(changeFloat, divisor)
				changeValue, _ := changeFloat.Float64()

				if changeValue < 0 {
					changeValue = -changeValue
				}

				if changeValue >= m.config.MinBalanceChangeNotification {
					// Record in history
					_, err = m.db.Exec(`
						INSERT INTO balance_history 
						(balance_id, account_id, network_id, network_token_id,
						 free_before, free_after, reserved_before, reserved_after,
						 total_before, total_after, change_amount, change_type, block_number)
						VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
					`, balanceID, account.ID, network.ID, nativeToken.ID,
						previousBalance.Free.String(), balance.Free.String(),
						previousBalance.Reserved.String(), balance.Reserved.String(),
						previousBalance.Total.String(), balance.Total.String(),
						change.String(), changeType, 0)

					// Send notification if enabled
					if m.discord != nil && account.DiscordNotify {
						m.discord.SendBalanceChangeNotification(
							account.Address, network.Name, nativeToken.Symbol,
							previousBalance.Total, balance.Total, changeType)
					}
				}
			}
		}

		// Calculate percentage change
		if accountBalance.TotalValue.Cmp(big.NewInt(0)) > 0 && accountBalance.ChangeValue.Cmp(big.NewInt(0)) != 0 {
			previousValue := new(big.Int).Sub(accountBalance.TotalValue, accountBalance.ChangeValue)
			if previousValue.Cmp(big.NewInt(0)) > 0 {
				changeFloat := new(big.Float).SetInt(accountBalance.ChangeValue)
				prevFloat := new(big.Float).SetInt(previousValue)
				percentFloat := new(big.Float).Quo(changeFloat, prevFloat)
				percentFloat.Mul(percentFloat, big.NewFloat(100))
				accountBalance.ChangePercent, _ = percentFloat.Float64()
			}
		}

		accountBalances[account.ID] = accountBalance
		totalPortfolioValue.Add(totalPortfolioValue, accountBalance.TotalValue)
		totalDailyRevenue.Add(totalDailyRevenue, accountBalance.ChangeValue)
	}

	// Generate and send daily summary
	m.sendDailySummary(accountBalances, totalPortfolioValue, totalDailyRevenue)

	log.Println("Balance check completed")
}

func (m *Monitor) sendDailySummary(accountBalances map[uint]*AccountBalance, totalValue, totalRevenue *big.Int) {
	if m.discord == nil {
		return
	}

	summary := discord.DailySummary{
		TotalAccounts:  len(accountBalances),
		ActiveNetworks: 0, // Will be calculated
	}

	// Calculate active networks
	networksUsed := make(map[string]bool)
	for _, ab := range accountBalances {
		for network := range ab.Balances {
			networksUsed[network] = true
		}
	}
	summary.ActiveNetworks = len(networksUsed)

	// Build account summaries
	for _, ab := range accountBalances {
		// Format values for display (assuming 10 decimals for now)
		divisor := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(10), nil))

		currentFloat := new(big.Float).SetInt(ab.TotalValue)
		currentFloat.Quo(currentFloat, divisor)
		currentValue, _ := currentFloat.Float64()

		changeFloat := new(big.Float).SetInt(ab.ChangeValue)
		changeFloat.Quo(changeFloat, divisor)
		changeValue, _ := changeFloat.Float64()

		// Build summary string
		summaryText := fmt.Sprintf("**Value:** %.4f | **Change:** %+.4f (%.2f%%)",
			currentValue, changeValue, ab.ChangePercent)

		// Add network breakdown if significant
		if len(ab.Balances) > 1 {
			summaryText += "\n    Networks: "
			networkList := ""
			for network, balance := range ab.Balances {
				if balance.Total.Cmp(big.NewInt(0)) > 0 {
					if networkList != "" {
						networkList += ", "
					}
					networkList += network
				}
			}
			summaryText += networkList
		}

		accountName := ab.Account.Name.String
		if !ab.Account.Name.Valid || ab.Account.Name.String == "" {
			accountName = fmt.Sprintf("%.8s...%.6s", ab.Account.Address,
				ab.Account.Address[len(ab.Account.Address)-6:])
		}

		summary.AccountSummaries = append(summary.AccountSummaries, discord.AccountSummary{
			Name:    accountName,
			Address: ab.Account.Address,
			Summary: summaryText,
		})
	}

	// Set revenue values
	summary.TotalPortfolioValue = totalValue
	summary.TotalDailyRevenue = totalRevenue

	// These will be filled by validator/collator/bounty checks
	summary.ChildBountyRevenue = big.NewInt(0)
	summary.ValidatorRevenue = big.NewInt(0)
	summary.CollatorRevenue = big.NewInt(0)
	summary.StakingRevenue = big.NewInt(0)

	// Send the summary
	m.discord.SendDailySummary(summary)
}

func (m *Monitor) StartValidatorMonitor(ctx context.Context, interval time.Duration) {
	// Run immediately
	m.checkValidators(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkValidators(ctx)
		}
	}
}

func (m *Monitor) checkValidators(ctx context.Context) {
	log.Println("Starting validator check...")
	// TODO: Implement validator checking logic
	log.Println("Validator check completed")
}

func (m *Monitor) StartBountyMonitor(ctx context.Context, interval time.Duration) {
	// Run immediately
	m.checkBounties(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkBounties(ctx)
		}
	}
}

func (m *Monitor) checkBounties(ctx context.Context) {
	log.Println("Starting bounty check...")
	// TODO: Implement bounty checking logic
	log.Println("Bounty check completed")
}
