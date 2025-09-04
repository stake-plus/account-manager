package monitor

import (
	"context"
	"database/sql"
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
	Network   string
	Balance   *big.Int
	Symbol    string
	Decimals  uint8
	Change    *big.Int
	TokenType string // native, asset, foreign_asset
}

type AccountBalance struct {
	Account        types.Account
	TokenBalances  []*discord.TokenBalance // All balances
	TotalsByToken  map[string]*big.Int     // symbol -> total across networks
	ChangesByToken map[string]*big.Int     // symbol -> change across networks
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
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Balance monitor panic recovered: %v", r)
		}
	}()

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
	log.Printf("Found %d accounts to monitor", len(accounts))

	networks, err := m.db.GetNetworks()
	if err != nil {
		log.Printf("Failed to get networks: %v", err)
		return
	}
	log.Printf("Found %d networks to check", len(networks))

	// Track all balances for daily summary
	accountBalances := make(map[uint]*AccountBalance)

	// Track portfolio totals by token
	portfolioTotalsByToken := make(map[string]*big.Int)  // symbol -> total value
	portfolioChangesByToken := make(map[string]*big.Int) // symbol -> total change

	processedAccounts := 0
	for _, account := range accounts {
		if !account.MonitorEnabled {
			log.Printf("Skipping disabled account: %s", account.Address)
			continue
		}

		log.Printf("Processing account %s (%s)", account.Name.String, account.Address)

		accountBalance := &AccountBalance{
			Account:        account,
			TokenBalances:  []*discord.TokenBalance{},
			TotalsByToken:  make(map[string]*big.Int),
			ChangesByToken: make(map[string]*big.Int),
		}

		for _, network := range networks {
			if !network.Active {
				continue
			}

			// Get native token balance
			balance, err := m.networks.GetBalance(network.Name, account.Address)
			if err != nil {
				log.Printf("  Failed to get balance for %s on %s: %v",
					account.Address, network.Name, err)
				continue
			}

			if balance.Total != nil && balance.Total.Cmp(big.NewInt(0)) > 0 {
				log.Printf("  %s balance on %s: %v", network.Symbol.String, network.Name, balance.Total)
			}

			// Get native token info
			var nativeToken types.NetworkToken
			err = m.db.QueryRow(`
				SELECT id, symbol, decimals FROM network_tokens 
				WHERE network_id = ? AND token_type = 'native'
			`, network.ID).Scan(&nativeToken.ID, &nativeToken.Symbol, &nativeToken.Decimals)

			if err != nil {
				log.Printf("  Failed to get native token for network %s: %v", network.Name, err)
				continue
			}

			// Process native token balance
			m.processTokenBalance(account, network, nativeToken, balance, accountBalance,
				portfolioTotalsByToken, portfolioChangesByToken, "native")

			// Check ALL asset tokens (remove the 5 asset limit)
			rows, err := m.db.Query(`
				SELECT id, symbol, decimals, token_id 
				FROM network_tokens 
				WHERE network_id = ? AND token_type IN ('asset', 'foreign_asset')
			`, network.ID)

			if err == nil && rows != nil {
				func() {
					defer rows.Close()

					checkedAssets := 0
					foundAssets := 0
					for rows.Next() {
						var assetToken types.NetworkToken
						var tokenID sql.NullString
						if err := rows.Scan(&assetToken.ID, &assetToken.Symbol, &assetToken.Decimals, &tokenID); err != nil {
							continue
						}

						if !tokenID.Valid || tokenID.String == "" {
							continue
						}

						checkedAssets++

						// Get asset balance for ALL assets
						assetBalance, err := m.networks.GetAssetBalance(network.Name, account.Address, tokenID.String)
						if err != nil {
							continue
						}

						if assetBalance.Total == nil || assetBalance.Total.Cmp(big.NewInt(0)) == 0 {
							continue
						}

						foundAssets++
						log.Printf("    Found %s balance: %v", assetToken.Symbol, assetBalance.Total)

						// Process asset balance
						tokenType := "asset"
						if strings.Contains(string(network.Name), "foreign") {
							tokenType = "foreign_asset"
						}

						m.processTokenBalance(account, network, assetToken, assetBalance, accountBalance,
							portfolioTotalsByToken, portfolioChangesByToken, tokenType)
					}

					if foundAssets > 0 {
						log.Printf("    Found %d non-zero asset balances out of %d checked", foundAssets, checkedAssets)
					}
				}()
			}
		}

		accountBalances[account.ID] = accountBalance
		processedAccounts++
	}

	log.Printf("Processed %d accounts, generating summary...", processedAccounts)

	// Generate and send daily summary
	if processedAccounts > 0 {
		m.sendDailySummary(accountBalances, portfolioTotalsByToken, portfolioChangesByToken)
	}

	log.Println("Balance check completed")
}

func (m *Monitor) processTokenBalance(account types.Account, network types.Network,
	token types.NetworkToken, balance types.Balance, accountBalance *AccountBalance,
	portfolioTotalsByToken, portfolioChangesByToken map[string]*big.Int, tokenType string) {

	defer func() {
		if r := recover(); r != nil {
			log.Printf("processTokenBalance panic for %s/%s: %v", account.Address, network.Name, r)
		}
	}()

	// Initialize balance values if nil
	if balance.Free == nil {
		balance.Free = big.NewInt(0)
	}
	if balance.Reserved == nil {
		balance.Reserved = big.NewInt(0)
	}
	if balance.MiscFrozen == nil {
		balance.MiscFrozen = big.NewInt(0)
	}
	if balance.FeeFrozen == nil {
		balance.FeeFrozen = big.NewInt(0)
	}
	if balance.Bonded == nil {
		balance.Bonded = big.NewInt(0)
	}
	if balance.Total == nil {
		balance.Total = big.NewInt(0)
	}

	// Check for balance changes - initialize previousBalance properly
	previousBalance := types.Balance{
		Free:       big.NewInt(0),
		Reserved:   big.NewInt(0),
		MiscFrozen: big.NewInt(0),
		FeeFrozen:  big.NewInt(0),
		Bonded:     big.NewInt(0),
		Total:      big.NewInt(0),
	}

	// Try to get previous balance
	var prevFree, prevReserved, prevMisc, prevFee, prevBonded, prevTotal string
	err := m.db.QueryRow(`
		SELECT free, reserved, misc_frozen, fee_frozen, bonded, total 
		FROM balances 
		WHERE account_id = ? AND network_id = ? AND network_token_id = ?
	`, account.ID, network.ID, token.ID).Scan(
		&prevFree, &prevReserved, &prevMisc, &prevFee, &prevBonded, &prevTotal,
	)

	balanceExists := err == nil

	// Parse previous balance strings if they exist
	if balanceExists {
		if val, ok := new(big.Int).SetString(prevFree, 10); ok && val != nil {
			previousBalance.Free = val
		}
		if val, ok := new(big.Int).SetString(prevReserved, 10); ok && val != nil {
			previousBalance.Reserved = val
		}
		if val, ok := new(big.Int).SetString(prevMisc, 10); ok && val != nil {
			previousBalance.MiscFrozen = val
		}
		if val, ok := new(big.Int).SetString(prevFee, 10); ok && val != nil {
			previousBalance.FeeFrozen = val
		}
		if val, ok := new(big.Int).SetString(prevBonded, 10); ok && val != nil {
			previousBalance.Bonded = val
		}
		if val, ok := new(big.Int).SetString(prevTotal, 10); ok && val != nil {
			previousBalance.Total = val
		}
	}

	change := new(big.Int).Sub(balance.Total, previousBalance.Total)

	// Store token balance info using discord.TokenBalance
	tokenBal := &discord.TokenBalance{
		Network:   network.Name,
		Balance:   new(big.Int).Set(balance.Total), // Create copy
		Symbol:    token.Symbol,
		Decimals:  token.Decimals,
		Change:    new(big.Int).Set(change), // Create copy
		TokenType: tokenType,
	}
	accountBalance.TokenBalances = append(accountBalance.TokenBalances, tokenBal)

	// Update totals by token - properly accumulate
	if accountBalance.TotalsByToken[token.Symbol] == nil {
		accountBalance.TotalsByToken[token.Symbol] = big.NewInt(0)
	}
	accountBalance.TotalsByToken[token.Symbol].Add(accountBalance.TotalsByToken[token.Symbol], balance.Total)

	if accountBalance.ChangesByToken[token.Symbol] == nil {
		accountBalance.ChangesByToken[token.Symbol] = big.NewInt(0)
	}
	accountBalance.ChangesByToken[token.Symbol].Add(accountBalance.ChangesByToken[token.Symbol], change)

	// Update portfolio totals - properly accumulate
	if portfolioTotalsByToken[token.Symbol] == nil {
		portfolioTotalsByToken[token.Symbol] = big.NewInt(0)
	}
	portfolioTotalsByToken[token.Symbol].Add(portfolioTotalsByToken[token.Symbol], balance.Total)

	if portfolioChangesByToken[token.Symbol] == nil {
		portfolioChangesByToken[token.Symbol] = big.NewInt(0)
	}
	portfolioChangesByToken[token.Symbol].Add(portfolioChangesByToken[token.Symbol], change)

	// Update database
	if balanceExists {
		_, err = m.db.Exec(`
			UPDATE balances SET 
				free = ?, reserved = ?, misc_frozen = ?, 
				fee_frozen = ?, bonded = ?, total = ?, 
				last_updated = NOW()
			WHERE account_id = ? AND network_id = ? AND network_token_id = ?
		`, balance.Free.String(), balance.Reserved.String(),
			balance.MiscFrozen.String(), balance.FeeFrozen.String(),
			balance.Bonded.String(), balance.Total.String(),
			account.ID, network.ID, token.ID)
		if err != nil {
			log.Printf("Failed to update balance: %v", err)
		}
	} else {
		_, err = m.db.Exec(`
			INSERT INTO balances 
			(account_id, network_id, network_token_id, free, reserved, 
			 misc_frozen, fee_frozen, bonded, total)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, account.ID, network.ID, token.ID,
			balance.Free.String(), balance.Reserved.String(),
			balance.MiscFrozen.String(), balance.FeeFrozen.String(),
			balance.Bonded.String(), balance.Total.String())
		if err != nil {
			log.Printf("Failed to insert balance: %v", err)
		}
	}

	// Send notification if significant change
	if change.Cmp(big.NewInt(0)) != 0 {
		changeType := "increase"
		if change.Cmp(big.NewInt(0)) < 0 {
			changeType = "decrease"
		}

		changeFloat := new(big.Float).SetInt(change)
		divisor := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(token.Decimals)), nil))
		changeFloat.Quo(changeFloat, divisor)

		changeValue, _ := changeFloat.Float64()
		if changeValue < 0 {
			changeValue = -changeValue
		}

		if changeValue >= m.config.MinBalanceChangeNotification && account.DiscordNotify {
			if m.discord != nil {
				err := m.discord.SendBalanceChangeNotification(
					account.Address, network.Name, token.Symbol,
					previousBalance.Total, balance.Total, changeType)
				if err != nil {
					log.Printf("Failed to send Discord notification: %v", err)
				}
			}
		}
	}
}

func (m *Monitor) sendDailySummary(accountBalances map[uint]*AccountBalance,
	portfolioTotalsByToken map[string]*big.Int,
	portfolioChangesByToken map[string]*big.Int) {

	log.Println("Preparing daily summary...")

	// Debug: Print portfolio totals
	for symbol, total := range portfolioTotalsByToken {
		log.Printf("Portfolio total for %s: %v", symbol, total)
	}

	if m.discord == nil {
		log.Println("Discord client is nil, cannot send summary")
		return
	}

	// Get token decimals map
	tokenDecimals := make(map[string]uint8)
	rows, err := m.db.Query(`
		SELECT DISTINCT symbol, decimals 
		FROM network_tokens
	`)
	if err == nil && rows != nil {
		defer rows.Close()
		for rows.Next() {
			var symbol string
			var decimals uint8
			if err := rows.Scan(&symbol, &decimals); err == nil {
				tokenDecimals[symbol] = decimals
			}
		}
	}

	summary := discord.DailySummary{
		TotalAccounts:    len(accountBalances),
		TotalsByToken:    make(map[string]*discord.TokenTotal),
		AccountSummaries: []discord.AccountSummary{},
		TokenDecimals:    tokenDecimals,
	}

	// Count active networks
	networksUsed := make(map[string]bool)
	for _, ab := range accountBalances {
		for _, tb := range ab.TokenBalances {
			if tb.Balance != nil && tb.Balance.Cmp(big.NewInt(0)) > 0 {
				networksUsed[tb.Network] = true
			}
		}
	}
	summary.ActiveNetworks = len(networksUsed)

	// Build token totals with proper values
	for symbol, total := range portfolioTotalsByToken {
		if total == nil {
			continue
		}

		change := portfolioChangesByToken[symbol]
		if change == nil {
			change = big.NewInt(0)
		}

		decimals := tokenDecimals[symbol]
		if decimals == 0 {
			decimals = 10
		}

		// Create copies of the big.Int values
		totalCopy := new(big.Int).Set(total)
		changeCopy := new(big.Int).Set(change)

		summary.TotalsByToken[symbol] = &discord.TokenTotal{
			Symbol:   symbol,
			Total:    totalCopy,
			Change:   changeCopy,
			Decimals: decimals,
		}

		log.Printf("Added to summary - %s: total=%v, change=%v", symbol, totalCopy, changeCopy)
	}

	// Build account summaries
	for _, ab := range accountBalances {
		accountName := ab.Account.Name.String
		if !ab.Account.Name.Valid || ab.Account.Name.String == "" {
			accountName = "Unknown"
		}

		// Create copies of the maps
		totalsCopy := make(map[string]*big.Int)
		for k, v := range ab.TotalsByToken {
			if v != nil {
				totalsCopy[k] = new(big.Int).Set(v)
			}
		}

		changesCopy := make(map[string]*big.Int)
		for k, v := range ab.ChangesByToken {
			if v != nil {
				changesCopy[k] = new(big.Int).Set(v)
			}
		}

		summary.AccountSummaries = append(summary.AccountSummaries, discord.AccountSummary{
			Name:           accountName,
			Address:        ab.Account.Address,
			TokenBalances:  ab.TokenBalances,
			TotalsByToken:  totalsCopy,
			ChangesByToken: changesCopy,
		})
	}

	// These will be filled by validator/collator/bounty checks
	summary.ChildBountyRevenue = big.NewInt(0)
	summary.ValidatorRevenue = big.NewInt(0)
	summary.CollatorRevenue = big.NewInt(0)
	summary.StakingRevenue = big.NewInt(0)

	// Send the summary
	log.Println("Sending daily summary to Discord...")
	err = m.discord.SendDailySummary(summary)
	if err != nil {
		log.Printf("Failed to send daily summary: %v", err)
	} else {
		log.Println("Daily summary sent successfully")
	}
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
