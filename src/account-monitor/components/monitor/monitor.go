package monitor

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"strings"
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

type TokenBalance struct {
	Balance  *big.Int
	Symbol   string
	Decimals uint8
	Change   *big.Int
}

type AccountBalance struct {
	Account        types.Account
	TokenBalances  map[string]*TokenBalance // "network:symbol" -> balance
	TotalsByToken  map[string]*big.Int      // symbol -> total across networks
	ChangesByToken map[string]*big.Int      // symbol -> change across networks
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

	// Track portfolio totals by token
	portfolioTotalsByToken := make(map[string]*big.Int)  // symbol -> total value
	portfolioChangesByToken := make(map[string]*big.Int) // symbol -> total change

	for _, account := range accounts {
		if !account.MonitorEnabled {
			continue
		}

		accountBalance := &AccountBalance{
			Account:        account,
			TokenBalances:  make(map[string]*TokenBalance),
			TotalsByToken:  make(map[string]*big.Int),
			ChangesByToken: make(map[string]*big.Int),
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

			// Store token balance info
			key := fmt.Sprintf("%s:%s", network.Name, nativeToken.Symbol)
			accountBalance.TokenBalances[key] = &TokenBalance{
				Balance:  balance.Total,
				Symbol:   nativeToken.Symbol,
				Decimals: nativeToken.Decimals,
				Change:   change,
			}

			// Update totals by token
			if accountBalance.TotalsByToken[nativeToken.Symbol] == nil {
				accountBalance.TotalsByToken[nativeToken.Symbol] = big.NewInt(0)
				accountBalance.ChangesByToken[nativeToken.Symbol] = big.NewInt(0)
			}
			accountBalance.TotalsByToken[nativeToken.Symbol].Add(accountBalance.TotalsByToken[nativeToken.Symbol], balance.Total)
			accountBalance.ChangesByToken[nativeToken.Symbol].Add(accountBalance.ChangesByToken[nativeToken.Symbol], change)

			// Update portfolio totals
			if portfolioTotalsByToken[nativeToken.Symbol] == nil {
				portfolioTotalsByToken[nativeToken.Symbol] = big.NewInt(0)
				portfolioChangesByToken[nativeToken.Symbol] = big.NewInt(0)
			}
			portfolioTotalsByToken[nativeToken.Symbol].Add(portfolioTotalsByToken[nativeToken.Symbol], balance.Total)
			portfolioChangesByToken[nativeToken.Symbol].Add(portfolioChangesByToken[nativeToken.Symbol], change)

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

		accountBalances[account.ID] = accountBalance
	}

	// Generate and send daily summary
	m.sendDailySummary(accountBalances, portfolioTotalsByToken, portfolioChangesByToken)

	log.Println("Balance check completed")
}

func (m *Monitor) sendDailySummary(accountBalances map[uint]*AccountBalance,
	portfolioTotalsByToken map[string]*big.Int,
	portfolioChangesByToken map[string]*big.Int) {

	if m.discord == nil {
		return
	}

	summary := discord.DailySummary{
		TotalAccounts:    len(accountBalances),
		ActiveNetworks:   0,
		TotalsByToken:    make(map[string]*discord.TokenTotal),
		AccountSummaries: []discord.AccountSummary{},
	}

	// Calculate active networks
	networksUsed := make(map[string]bool)
	for _, ab := range accountBalances {
		for key := range ab.TokenBalances {
			network := key[:strings.Index(key, ":")]
			networksUsed[network] = true
		}
	}
	summary.ActiveNetworks = len(networksUsed)

	// Get token decimals map
	tokenDecimals := make(map[string]uint8)
	rows, _ := m.db.Query(`
		SELECT DISTINCT symbol, decimals 
		FROM network_tokens 
		WHERE token_type = 'native'
	`)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var symbol string
			var decimals uint8
			if err := rows.Scan(&symbol, &decimals); err == nil {
				tokenDecimals[symbol] = decimals
			}
		}
	}

	// Build token totals
	for symbol, total := range portfolioTotalsByToken {
		change := portfolioChangesByToken[symbol]
		if change == nil {
			change = big.NewInt(0)
		}

		decimals := tokenDecimals[symbol]
		if decimals == 0 {
			decimals = 10 // default
		}

		summary.TotalsByToken[symbol] = &discord.TokenTotal{
			Symbol:   symbol,
			Total:    total,
			Change:   change,
			Decimals: decimals,
		}
	}

	// Build account summaries
	for _, ab := range accountBalances {
		var summaryLines []string

		// Show balances by token
		for symbol, total := range ab.TotalsByToken {
			if total.Cmp(big.NewInt(0)) == 0 {
				continue
			}

			change := ab.ChangesByToken[symbol]
			if change == nil {
				change = big.NewInt(0)
			}

			decimals := tokenDecimals[symbol]
			if decimals == 0 {
				decimals = 10
			}

			divisor := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))

			totalFloat := new(big.Float).SetInt(total)
			totalFloat.Quo(totalFloat, divisor)
			totalValue, _ := totalFloat.Float64()

			changeFloat := new(big.Float).SetInt(change)
			changeFloat.Quo(changeFloat, divisor)
			changeValue, _ := changeFloat.Float64()

			changePercent := 0.0
			if totalValue > 0 && changeValue != 0 {
				previousValue := totalValue - changeValue
				if previousValue > 0 {
					changePercent = (changeValue / previousValue) * 100
				}
			}

			summaryLines = append(summaryLines,
				fmt.Sprintf("**%s:** %.4f | Change: %+.4f (%.2f%%)",
					symbol, totalValue, changeValue, changePercent))
		}

		// Add network breakdown
		networkList := ""
		for key := range ab.TokenBalances {
			if ab.TokenBalances[key].Balance.Cmp(big.NewInt(0)) > 0 {
				parts := strings.Split(key, ":")
				if networkList != "" {
					networkList += ", "
				}
				networkList += parts[0]
			}
		}
		if networkList != "" {
			summaryLines = append(summaryLines, fmt.Sprintf("Networks: %s", networkList))
		}

		accountName := ab.Account.Name.String
		if !ab.Account.Name.Valid || ab.Account.Name.String == "" {
			accountName = fmt.Sprintf("%.8s...%.6s", ab.Account.Address,
				ab.Account.Address[len(ab.Account.Address)-6:])
		}

		summary.AccountSummaries = append(summary.AccountSummaries, discord.AccountSummary{
			Name:    accountName,
			Address: ab.Account.Address,
			Summary: strings.Join(summaryLines, "\n"),
		})
	}

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
