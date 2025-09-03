package database

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
	types "github.com/stake-plus/account-manager/src/account-monitor/components/types"
)

type DB struct {
	*sql.DB
}

func Initialize(dsn string) (*DB, error) {
	db, err := sql.Open("mysql", dsn+"?parseTime=true")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{db}, nil
}

func (db *DB) Close() error {
	return db.DB.Close()
}

// LoadSettings loads all settings from the database
func LoadSettings(db *DB) (map[string]string, error) {
	settings := make(map[string]string)

	rows, err := db.Query("SELECT name, value FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			continue
		}
		settings[name] = value
	}

	return settings, nil
}

// GetNetworks retrieves all active networks
func (db *DB) GetNetworks() ([]types.Network, error) {
	var networks []types.Network

	rows, err := db.Query(`
		SELECT id, name, display_name, network_type, rpc_url, ws_url, 
		       decimals, symbol, ss58_prefix, active, last_checked_block
		FROM networks
		WHERE active = TRUE
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var n types.Network
		err := rows.Scan(&n.ID, &n.Name, &n.DisplayName, &n.NetworkType,
			&n.RPCURL, &n.WSURL, &n.Decimals, &n.Symbol, &n.SS58Prefix,
			&n.Active, &n.LastCheckedBlock)
		if err != nil {
			continue
		}
		networks = append(networks, n)
	}

	return networks, nil
}

// GetAccounts retrieves all monitored accounts
func (db *DB) GetAccounts() ([]types.Account, error) {
	var accounts []types.Account

	rows, err := db.Query(`
		SELECT id, address, address_type, name, description, 
		       monitor_enabled, discord_notify
		FROM accounts
		WHERE monitor_enabled = TRUE
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var a types.Account
		err := rows.Scan(&a.ID, &a.Address, &a.AddressType, &a.Name,
			&a.Description, &a.MonitorEnabled, &a.DiscordNotify)
		if err != nil {
			continue
		}
		accounts = append(accounts, a)
	}

	return accounts, nil
}

// UpdateBalance updates or inserts a balance record
func (db *DB) UpdateBalance(accountID, networkID, tokenID uint, balance types.Balance) error {
	_, err := db.Exec(`
		INSERT INTO balances (account_id, network_id, network_token_id, free, reserved, 
		                     misc_frozen, fee_frozen, bonded, total)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
		free = VALUES(free),
		reserved = VALUES(reserved),
		misc_frozen = VALUES(misc_frozen),
		fee_frozen = VALUES(fee_frozen),
		bonded = VALUES(bonded),
		total = VALUES(total),
		last_updated = CURRENT_TIMESTAMP
	`, accountID, networkID, tokenID, balance.Free.String(), balance.Reserved.String(),
		balance.MiscFrozen.String(), balance.FeeFrozen.String(), balance.Bonded.String(), balance.Total.String())

	return err
}

// RecordBalanceChange records a balance change in history
func (db *DB) RecordBalanceChange(change types.BalanceChange) error {
	_, err := db.Exec(`
		INSERT INTO balance_history (balance_id, account_id, network_id, network_token_id,
		                            free_before, free_after, total_before, total_after,
		                            change_amount, change_type, tx_hash, block_number)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, change.BalanceID, change.AccountID, change.NetworkID, change.TokenID,
		change.FreeBefore.String(), change.FreeAfter.String(), change.TotalBefore.String(), change.TotalAfter.String(),
		change.ChangeAmount.String(), change.ChangeType, change.TxHash.String, change.BlockNumber.Int64)

	return err
}
