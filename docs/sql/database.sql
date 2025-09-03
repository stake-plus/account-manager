-- Account Monitor Database Schema
-- For tracking Polkadot/Substrate network accounts, balances, and rewards

-- Drop existing tables in correct order
DROP TABLE IF EXISTS validator_era_stats;
DROP TABLE IF EXISTS validator_stats;
DROP TABLE IF EXISTS collator_stats;
DROP TABLE IF EXISTS balance_history;
DROP TABLE IF EXISTS balances;
DROP TABLE IF EXISTS child_bounties;
DROP TABLE IF EXISTS bounties;
DROP TABLE IF EXISTS account_roles;
DROP TABLE IF EXISTS accounts;
DROP TABLE IF EXISTS network_tokens;
DROP TABLE IF EXISTS network_pallets;
DROP TABLE IF EXISTS networks;
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS block_cache;

-- Settings table for Discord API and other configuration
CREATE TABLE IF NOT EXISTS settings (
    id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(64) NOT NULL UNIQUE,
    value TEXT NOT NULL,
    description VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Networks table
CREATE TABLE IF NOT EXISTS networks (
    id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(64) NOT NULL UNIQUE,
    display_name VARCHAR(128),
    network_type ENUM('substrate', 'substrate-evm') DEFAULT 'substrate',
    rpc_url VARCHAR(255) NOT NULL,
    ws_url VARCHAR(255),
    decimals TINYINT UNSIGNED DEFAULT 10,
    symbol VARCHAR(16),
    ss58_prefix SMALLINT UNSIGNED DEFAULT 42,
    active BOOLEAN DEFAULT TRUE,
    last_checked_block BIGINT UNSIGNED DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_active (active),
    INDEX idx_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Network pallets detection
CREATE TABLE IF NOT EXISTS network_pallets (
    id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    network_id INT UNSIGNED NOT NULL,
    pallet_name VARCHAR(64) NOT NULL,
    pallet_index SMALLINT UNSIGNED,
    detected BOOLEAN DEFAULT TRUE,
    metadata JSON,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_network_pallet (network_id, pallet_name),
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    INDEX idx_network_pallet (network_id, pallet_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Network tokens (native and additional tokens)
CREATE TABLE IF NOT EXISTS network_tokens (
    id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    network_id INT UNSIGNED NOT NULL,
    token_type ENUM('native', 'asset', 'foreign_asset', 'orml') DEFAULT 'native',
    token_id VARCHAR(64), -- Asset ID for non-native tokens
    symbol VARCHAR(16) NOT NULL,
    name VARCHAR(128),
    decimals TINYINT UNSIGNED NOT NULL,
    pallet_name VARCHAR(64), -- Which pallet manages this token
    metadata JSON,
    active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_network_token (network_id, token_type, COALESCE(token_id, '')),
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    INDEX idx_network_active (network_id, active)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Accounts table
CREATE TABLE IF NOT EXISTS accounts (
    id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    address VARCHAR(128) NOT NULL UNIQUE,
    address_type ENUM('substrate', 'evm') DEFAULT 'substrate',
    name VARCHAR(128),
    description TEXT,
    monitor_enabled BOOLEAN DEFAULT TRUE,
    discord_notify BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_address (address),
    INDEX idx_monitor (monitor_enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Account roles (validator, nominator, collator)
CREATE TABLE IF NOT EXISTS account_roles (
    id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    account_id INT UNSIGNED NOT NULL,
    network_id INT UNSIGNED NOT NULL,
    role_type ENUM('validator', 'nominator', 'collator') NOT NULL,
    stash_address VARCHAR(128),
    controller_address VARCHAR(128),
    active BOOLEAN DEFAULT TRUE,
    metadata JSON,
    detected_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_account_network_role (account_id, network_id, role_type),
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    INDEX idx_role_type (role_type),
    INDEX idx_active (active)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Bounties table
CREATE TABLE IF NOT EXISTS bounties (
    id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    network_id INT UNSIGNED NOT NULL,
    bounty_id BIGINT UNSIGNED NOT NULL,
    proposer VARCHAR(128),
    curator VARCHAR(128),
    fee DECIMAL(40, 0),
    curator_deposit DECIMAL(40, 0),
    bond DECIMAL(40, 0),
    value DECIMAL(40, 0),
    status ENUM('proposed', 'approved', 'funded', 'curator_proposed', 'active', 'pending_payout', 'closed') DEFAULT 'proposed',
    description TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_network_bounty (network_id, bounty_id),
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    INDEX idx_status (status),
    INDEX idx_curator (curator)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Child bounties table
CREATE TABLE IF NOT EXISTS child_bounties (
    id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    bounty_id INT UNSIGNED NOT NULL,
    child_bounty_id BIGINT UNSIGNED NOT NULL,
    network_token_id INT UNSIGNED NOT NULL,
    curator_address VARCHAR(128),
    beneficiary_address VARCHAR(128),
    value DECIMAL(40, 0),
    fee DECIMAL(40, 0),
    status ENUM('added', 'curator_proposed', 'active', 'pending_payout', 'awarded', 'claimed', 'canceled') DEFAULT 'added',
    description TEXT,
    awarded_at TIMESTAMP NULL,
    claimed_at TIMESTAMP NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (bounty_id) REFERENCES bounties(id) ON DELETE CASCADE,
    FOREIGN KEY (network_token_id) REFERENCES network_tokens(id),
    INDEX idx_status (status),
    INDEX idx_beneficiary (beneficiary_address),
    INDEX idx_curator (curator_address)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Balances table
CREATE TABLE IF NOT EXISTS balances (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    account_id INT UNSIGNED NOT NULL,
    network_id INT UNSIGNED NOT NULL,
    network_token_id INT UNSIGNED NOT NULL,
    free DECIMAL(40, 0) DEFAULT 0,
    reserved DECIMAL(40, 0) DEFAULT 0,
    misc_frozen DECIMAL(40, 0) DEFAULT 0,
    fee_frozen DECIMAL(40, 0) DEFAULT 0,
    bonded DECIMAL(40, 0) DEFAULT 0,
    total DECIMAL(40, 0) DEFAULT 0,
    last_updated TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_account_network_token (account_id, network_id, network_token_id),
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    FOREIGN KEY (network_token_id) REFERENCES network_tokens(id),
    INDEX idx_account_network (account_id, network_id),
    INDEX idx_last_updated (last_updated)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Balance history for tracking changes
CREATE TABLE IF NOT EXISTS balance_history (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    balance_id BIGINT UNSIGNED NOT NULL,
    account_id INT UNSIGNED NOT NULL,
    network_id INT UNSIGNED NOT NULL,
    network_token_id INT UNSIGNED NOT NULL,
    free_before DECIMAL(40, 0),
    free_after DECIMAL(40, 0),
    total_before DECIMAL(40, 0),
    total_after DECIMAL(40, 0),
    change_amount DECIMAL(40, 0),
    change_type ENUM('increase', 'decrease', 'no_change') DEFAULT 'no_change',
    tx_hash VARCHAR(128),
    block_number BIGINT UNSIGNED,
    recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (balance_id) REFERENCES balances(id) ON DELETE CASCADE,
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    FOREIGN KEY (network_token_id) REFERENCES network_tokens(id),
    INDEX idx_account_time (account_id, recorded_at),
    INDEX idx_change_type (change_type),
    INDEX idx_recorded_at (recorded_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Validator statistics
CREATE TABLE IF NOT EXISTS validator_stats (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    account_id INT UNSIGNED NOT NULL,
    network_id INT UNSIGNED NOT NULL,
    total_stake DECIMAL(40, 0),
    self_stake DECIMAL(40, 0),
    nominator_count INT UNSIGNED DEFAULT 0,
    commission_percent DECIMAL(5, 2),
    first_seen_era INT UNSIGNED,
    last_active_era INT UNSIGNED,
    total_slash_count INT UNSIGNED DEFAULT 0,
    total_slash_amount DECIMAL(40, 0) DEFAULT 0,
    unclaimed_eras JSON,
    unclaimed_amount DECIMAL(40, 0) DEFAULT 0,
    expired_unclaimed_amount DECIMAL(40, 0) DEFAULT 0,
    top_nominators JSON,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_account_network (account_id, network_id),
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    INDEX idx_network (network_id),
    INDEX idx_updated (updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Validator era statistics
CREATE TABLE IF NOT EXISTS validator_era_stats (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    account_id INT UNSIGNED NOT NULL,
    network_id INT UNSIGNED NOT NULL,
    era INT UNSIGNED NOT NULL,
    active BOOLEAN DEFAULT FALSE,
    points INT UNSIGNED DEFAULT 0,
    rewards DECIMAL(40, 0) DEFAULT 0,
    claimed BOOLEAN DEFAULT FALSE,
    slash_amount DECIMAL(40, 0) DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_account_network_era (account_id, network_id, era),
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    INDEX idx_era (era),
    INDEX idx_claimed (claimed)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Collator statistics
CREATE TABLE IF NOT EXISTS collator_stats (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    account_id INT UNSIGNED NOT NULL,
    network_id INT UNSIGNED NOT NULL,
    total_stake DECIMAL(40, 0),
    delegator_count INT UNSIGNED DEFAULT 0,
    blocks_produced INT UNSIGNED DEFAULT 0,
    last_block_produced BIGINT UNSIGNED,
    unclaimed_rewards DECIMAL(40, 0) DEFAULT 0,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_account_network (account_id, network_id),
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    INDEX idx_network (network_id),
    INDEX idx_updated (updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Block cache for efficient processing
CREATE TABLE IF NOT EXISTS block_cache (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    network_id INT UNSIGNED NOT NULL,
    block_number BIGINT UNSIGNED NOT NULL,
    block_hash VARCHAR(128),
    parent_hash VARCHAR(128),
    extrinsics_root VARCHAR(128),
    state_root VARCHAR(128),
    processed BOOLEAN DEFAULT FALSE,
    extrinsics JSON,
    events JSON,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_network_block (network_id, block_number),
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    INDEX idx_processed (processed),
    INDEX idx_block_number (block_number)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Insert default settings
INSERT INTO settings (name, value, description) VALUES
    ('discord_webhook_url', '', 'Discord webhook URL for notifications'),
    ('discord_channel_id', '', 'Discord channel ID for alerts'),
    ('check_interval_hours', '24', 'Hours between balance checks'),
    ('validator_check_interval_hours', '8', 'Hours between validator checks'),
    ('enable_notifications', 'true', 'Enable Discord notifications'),
    ('min_balance_change_notification', '0.0001', 'Minimum balance change to trigger notification');