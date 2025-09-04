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
				log.Printf("Failed to get balance for %s on %s: %v", account.Address, network.Name, err)
				continue
			}

			// Get native token info
			var nativeToken types.NetworkToken
			err = m.db.QueryRow(`
				SELECT id, symbol, decimals FROM network_tokens 
				WHERE network_id = ? AND token_type = 'native'
			`, network.ID).Scan(&nativeToken.ID, &nativeToken.Symbol, &nativeToken.Decimals)

			if err != nil {
				log.Printf("Failed to get native token for network %s: %v", network.Name, err)
				continue
			}

			// Process native token balance
			m.processTokenBalance(account, network, nativeToken, balance, accountBalance,
				portfolioTotalsByToken, portfolioChangesByToken, "native")

			// Get all asset tokens for this network
			rows, err := m.db.Query(`
				SELECT id, symbol, decimals, token_id 
				FROM network_tokens 
				WHERE network_id = ? AND token_type IN ('asset', 'foreign_asset')
			`, network.ID)

			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var assetToken types.NetworkToken
					var tokenID sql.NullString
					if err := rows.Scan(&assetToken.ID, &assetToken.Symbol, &assetToken.Decimals, &tokenID); err != nil {
						continue
					}

					// Get asset balance
					assetBalance, err := m.networks.GetAssetBalance(network.Name, account.Address, tokenID.String)
					if err != nil || assetBalance.Total.Cmp(big.NewInt(0)) == 0 {
						continue
					}

					// Process asset balance
					tokenType := "asset"
					if strings.Contains(assetToken.Symbol, "foreign") {
						tokenType = "foreign_asset"
					}
					m.processTokenBalance(account, network, assetToken, assetBalance, accountBalance,
						portfolioTotalsByToken, portfolioChangesByToken, tokenType)
				}
			}
		}

		accountBalances[account.ID] = accountBalance
	}

	// Generate and send daily summary
	m.sendDailySummary(accountBalances, portfolioTotalsByToken, portfolioChangesByToken)

	log.Println("Balance check completed")
}

func (m *Monitor) processTokenBalance(account types.Account, network types.Network,
	token types.NetworkToken, balance types.Balance, accountBalance *AccountBalance,
	portfolioTotalsByToken, portfolioChangesByToken map[string]*big.Int, tokenType string) {

	// Check for balance changes
	var previousBalance types.Balance
	err := m.db.QueryRow(`
		SELECT free, reserved, misc_frozen, fee_frozen, bonded, total 
		FROM balances 
		WHERE account_id = ? AND network_id = ? AND network_token_id = ?
	`, account.ID, network.ID, token.ID).Scan(
		&previousBalance.Free, &previousBalance.Reserved,
		&previousBalance.MiscFrozen, &previousBalance.FeeFrozen,
		&previousBalance.Bonded, &previousBalance.Total,
	)

	balanceExists := err == nil
	change := new(big.Int).Sub(balance.Total, previousBalance.Total)

	// Store token balance info using discord.TokenBalance
	tokenBal := &discord.TokenBalance{
		Network:   network.Name,
		Balance:   balance.Total,
		Symbol:    token.Symbol,
		Decimals:  token.Decimals,
		Change:    change,
		TokenType: tokenType,
	}
	accountBalance.TokenBalances = append(accountBalance.TokenBalances, tokenBal)

	// Rest of the function remains the same...
	// Update totals by token
	if accountBalance.TotalsByToken[token.Symbol] == nil {
		accountBalance.TotalsByToken[token.Symbol] = big.NewInt(0)
		accountBalance.ChangesByToken[token.Symbol] = big.NewInt(0)
	}
	accountBalance.TotalsByToken[token.Symbol].Add(accountBalance.TotalsByToken[token.Symbol], balance.Total)
	accountBalance.ChangesByToken[token.Symbol].Add(accountBalance.ChangesByToken[token.Symbol], change)

	// Update portfolio totals
	if portfolioTotalsByToken[token.Symbol] == nil {
		portfolioTotalsByToken[token.Symbol] = big.NewInt(0)
		portfolioChangesByToken[token.Symbol] = big.NewInt(0)
	}
	portfolioTotalsByToken[token.Symbol].Add(portfolioTotalsByToken[token.Symbol], balance.Total)
	portfolioChangesByToken[token.Symbol].Add(portfolioChangesByToken[token.Symbol], change)

	// Update database
	if balanceExists {
		m.db.Exec(`
			UPDATE balances SET 
				free = ?, reserved = ?, misc_frozen = ?, 
				fee_frozen = ?, bonded = ?, total = ?, 
				last_updated = NOW()
			WHERE account_id = ? AND network_id = ? AND network_token_id = ?
		`, balance.Free.String(), balance.Reserved.String(),
			balance.MiscFrozen.String(), balance.FeeFrozen.String(),
			balance.Bonded.String(), balance.Total.String(),
			account.ID, network.ID, token.ID)
	} else {
		m.db.Exec(`
			INSERT INTO balances 
			(account_id, network_id, network_token_id, free, reserved, 
			 misc_frozen, fee_frozen, bonded, total)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, account.ID, network.ID, token.ID,
			balance.Free.String(), balance.Reserved.String(),
			balance.MiscFrozen.String(), balance.FeeFrozen.String(),
			balance.Bonded.String(), balance.Total.String())
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
				m.discord.SendBalanceChangeNotification(
					account.Address, network.Name, token.Symbol,
					previousBalance.Total, balance.Total, changeType)
			}
		}
	}
}

func (m *Monitor) sendDailySummary(accountBalances map[uint]*AccountBalance,
	portfolioTotalsByToken map[string]*big.Int,
	portfolioChangesByToken map[string]*big.Int) {

	if m.discord == nil {
		return
	}

	// Get token decimals map
	tokenDecimals := make(map[string]uint8)
	rows, _ := m.db.Query(`
		SELECT DISTINCT symbol, decimals 
		FROM network_tokens
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
			if tb.Balance.Cmp(big.NewInt(0)) > 0 {
				networksUsed[tb.Network] = true
			}
		}
	}
	summary.ActiveNetworks = len(networksUsed)

	// Build token totals
	for symbol, total := range portfolioTotalsByToken {
		change := portfolioChangesByToken[symbol]
		if change == nil {
			change = big.NewInt(0)
		}

		decimals := tokenDecimals[symbol]
		if decimals == 0 {
			decimals = 10
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
		accountName := ab.Account.Name.String
		if !ab.Account.Name.Valid || ab.Account.Name.String == "" {
			accountName = "Unknown"
		}

		summary.AccountSummaries = append(summary.AccountSummaries, discord.AccountSummary{
			Name:           accountName,
			Address:        ab.Account.Address,
			TokenBalances:  ab.TokenBalances,
			TotalsByToken:  ab.TotalsByToken,
			ChangesByToken: ab.ChangesByToken,
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
