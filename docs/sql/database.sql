-- Account Monitor Database Schema
-- Version 1.0.0

-- Drop existing tables if needed (in correct order due to foreign keys)
DROP TABLE IF EXISTS validator_era_stats;
DROP TABLE IF EXISTS validator_stats;
DROP TABLE IF EXISTS account_roles;
DROP TABLE IF EXISTS child_bounties;
DROP TABLE IF EXISTS bounties;
DROP TABLE IF EXISTS balance_history;
DROP TABLE IF EXISTS balances;
DROP TABLE IF EXISTS network_tokens;
DROP TABLE IF EXISTS network_pallets;
DROP TABLE IF EXISTS accounts;
DROP TABLE IF EXISTS network_rpcs;
DROP TABLE IF EXISTS networks;
DROP TABLE IF EXISTS settings;

-- Settings table for storing configuration
CREATE TABLE settings (
    id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(64) NOT NULL UNIQUE,
    value TEXT,
    description VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_settings_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Networks table
CREATE TABLE networks (
    id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(64) NOT NULL UNIQUE,
    display_name VARCHAR(64),
    network_type ENUM('substrate', 'substrate-evm') NOT NULL DEFAULT 'substrate',
    rpc_url VARCHAR(255) NOT NULL,
    ws_url VARCHAR(255),
    decimals TINYINT UNSIGNED DEFAULT 10,
    symbol VARCHAR(10),
    ss58_prefix SMALLINT UNSIGNED DEFAULT 42,
    active BOOLEAN DEFAULT TRUE,
    last_checked_block BIGINT UNSIGNED DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_networks_active (active),
    INDEX idx_networks_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Network RPC endpoints (multiple per network)
CREATE TABLE network_rpcs (
    id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    network_id INT UNSIGNED NOT NULL,
    url VARCHAR(255) NOT NULL,
    type ENUM('http', 'ws') DEFAULT 'ws',
    priority INT DEFAULT 0,
    active BOOLEAN DEFAULT TRUE,
    last_checked TIMESTAMP NULL,
    response_time_ms INT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    INDEX idx_rpcs_network (network_id, active)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Network pallets detection
CREATE TABLE network_pallets (
    id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    network_id INT UNSIGNED NOT NULL,
    pallet_name VARCHAR(64) NOT NULL,
    pallet_index INT,
    detected BOOLEAN DEFAULT FALSE,
    version VARCHAR(32),
    last_checked TIMESTAMP NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    UNIQUE KEY unique_network_pallet (network_id, pallet_name),
    INDEX idx_pallets_network (network_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Accounts table
CREATE TABLE accounts (
    id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    address VARCHAR(128) NOT NULL UNIQUE,
    address_type ENUM('substrate', 'evm') NOT NULL DEFAULT 'substrate',
    name VARCHAR(128),
    description TEXT,
    monitor_enabled BOOLEAN DEFAULT TRUE,
    discord_notify BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_accounts_enabled (monitor_enabled),
    INDEX idx_accounts_address (address)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Network tokens (native and assets)
CREATE TABLE network_tokens (
    id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    network_id INT UNSIGNED NOT NULL,
    token_type ENUM('native', 'asset', 'foreign_asset', 'orml') NOT NULL,
    token_id VARCHAR(64),
    symbol VARCHAR(32) NOT NULL,
    name VARCHAR(128),
    decimals TINYINT UNSIGNED NOT NULL,
    pallet_name VARCHAR(64),
    metadata JSON,
    active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    UNIQUE KEY unique_network_token (network_id, token_type, token_id),
    INDEX idx_tokens_network (network_id, active)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Balances table
CREATE TABLE balances (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    account_id INT UNSIGNED NOT NULL,
    network_id INT UNSIGNED NOT NULL,
    network_token_id INT UNSIGNED NOT NULL,
    free DECIMAL(65,0) DEFAULT 0,
    reserved DECIMAL(65,0) DEFAULT 0,
    misc_frozen DECIMAL(65,0) DEFAULT 0,
    fee_frozen DECIMAL(65,0) DEFAULT 0,
    bonded DECIMAL(65,0) DEFAULT 0,
    total DECIMAL(65,0) DEFAULT 0,
    last_updated TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    FOREIGN KEY (network_token_id) REFERENCES network_tokens(id) ON DELETE CASCADE,
    UNIQUE KEY unique_account_network_token (account_id, network_id, network_token_id),
    INDEX idx_balances_account (account_id),
    INDEX idx_balances_network (network_id),
    INDEX idx_balances_updated (last_updated)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Balance history for tracking changes
CREATE TABLE balance_history (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    balance_id BIGINT UNSIGNED NOT NULL,
    account_id INT UNSIGNED NOT NULL,
    network_id INT UNSIGNED NOT NULL,
    network_token_id INT UNSIGNED NOT NULL,
    free_before DECIMAL(65,0),
    free_after DECIMAL(65,0),
    reserved_before DECIMAL(65,0),
    reserved_after DECIMAL(65,0),
    total_before DECIMAL(65,0),
    total_after DECIMAL(65,0),
    change_amount DECIMAL(65,0),
    change_type ENUM('increase', 'decrease', 'claim', 'slash', 'reward') NOT NULL,
    tx_hash VARCHAR(128),
    block_number BIGINT UNSIGNED,
    recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (balance_id) REFERENCES balances(id) ON DELETE CASCADE,
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    FOREIGN KEY (network_token_id) REFERENCES network_tokens(id) ON DELETE CASCADE,
    INDEX idx_history_account (account_id, recorded_at),
    INDEX idx_history_network (network_id, recorded_at),
    INDEX idx_history_type (change_type),
    INDEX idx_history_date (recorded_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Bounties table
CREATE TABLE bounties (
    id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    network_id INT UNSIGNED NOT NULL,
    bounty_id BIGINT UNSIGNED NOT NULL,
    proposer VARCHAR(128),
    curator VARCHAR(128),
    fee DECIMAL(65,0),
    curator_deposit DECIMAL(65,0),
    bond DECIMAL(65,0),
    value DECIMAL(65,0),
    status VARCHAR(32),
    description TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    UNIQUE KEY unique_network_bounty (network_id, bounty_id),
    INDEX idx_bounties_network (network_id),
    INDEX idx_bounties_curator (curator),
    INDEX idx_bounties_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Child bounties table
CREATE TABLE child_bounties (
    id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    bounty_id INT UNSIGNED NOT NULL,
    child_bounty_id BIGINT UNSIGNED NOT NULL,
    network_token_id INT UNSIGNED NOT NULL,
    curator_address VARCHAR(128),
    beneficiary_address VARCHAR(128),
    value DECIMAL(65,0),
    fee DECIMAL(65,0),
    status ENUM('added', 'curator_proposed', 'active', 'pending_award', 'awarded', 'claimed', 'cancelled') NOT NULL,
    description TEXT,
    awarded_at TIMESTAMP NULL,
    claimed_at TIMESTAMP NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (bounty_id) REFERENCES bounties(id) ON DELETE CASCADE,
    FOREIGN KEY (network_token_id) REFERENCES network_tokens(id),
    UNIQUE KEY unique_bounty_child (bounty_id, child_bounty_id),
    INDEX idx_child_bounties_status (status),
    INDEX idx_child_bounties_beneficiary (beneficiary_address),
    INDEX idx_child_bounties_curator (curator_address)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Account roles (validator, nominator, collator)
CREATE TABLE account_roles (
    id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    account_id INT UNSIGNED NOT NULL,
    network_id INT UNSIGNED NOT NULL,
    role_type ENUM('validator', 'nominator', 'collator', 'delegator') NOT NULL,
    stash_address VARCHAR(128),
    controller_address VARCHAR(128),
    active BOOLEAN DEFAULT TRUE,
    metadata JSON,
    detected_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_checked TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    UNIQUE KEY unique_account_network_role (account_id, network_id, role_type),
    INDEX idx_roles_account (account_id),
    INDEX idx_roles_network (network_id),
    INDEX idx_roles_type (role_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Validator statistics
CREATE TABLE validator_stats (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    account_id INT UNSIGNED NOT NULL,
    network_id INT UNSIGNED NOT NULL,
    total_stake DECIMAL(65,0),
    self_stake DECIMAL(65,0),
    nominator_count INT UNSIGNED DEFAULT 0,
    commission_percent DECIMAL(5,2),
    first_seen_era INT UNSIGNED,
    last_active_era INT UNSIGNED,
    total_slash_count INT UNSIGNED DEFAULT 0,
    total_slash_amount DECIMAL(65,0) DEFAULT 0,
    unclaimed_eras JSON,
    unclaimed_amount DECIMAL(65,0),
    expired_unclaimed_amount DECIMAL(65,0),
    top_nominators JSON,
    last_updated TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    UNIQUE KEY unique_validator_network (account_id, network_id),
    INDEX idx_validator_stats_account (account_id),
    INDEX idx_validator_stats_network (network_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Validator era statistics
CREATE TABLE validator_era_stats (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    account_id INT UNSIGNED NOT NULL,
    network_id INT UNSIGNED NOT NULL,
    era INT UNSIGNED NOT NULL,
    active BOOLEAN DEFAULT FALSE,
    points INT UNSIGNED DEFAULT 0,
    rewards_claimed BOOLEAN DEFAULT FALSE,
    reward_amount DECIMAL(65,0),
    slash_amount DECIMAL(65,0) DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    UNIQUE KEY unique_validator_era (account_id, network_id, era),
    INDEX idx_era_stats_account (account_id),
    INDEX idx_era_stats_network (network_id),
    INDEX idx_era_stats_era (era)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Insert default settings
INSERT INTO settings (name, value, description) VALUES
('discord_webhook', '', 'Discord webhook URL for notifications'),
('discord_channel_id', '', 'Discord channel ID for notifications'),
('discord_token', '', 'Discord bot token'),
('guild_id', '', 'Discord guild ID'),
('alerts_channel_id', '', 'Discord channel for alerts'),
('summary_channel_id', '', 'Discord channel for daily summaries'),
('check_interval_hours', '24', 'Hours between balance checks'),
('validator_check_interval_hours', '8', 'Hours between validator checks'),
('bounty_check_interval_minutes', '30', 'Minutes between bounty checks'),
('min_balance_change_notification', '0.0001', 'Minimum balance change to trigger notification'),
('enable_notifications', 'true', 'Enable Discord notifications');