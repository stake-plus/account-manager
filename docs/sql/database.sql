CREATE DATABASE IF NOT EXISTS account_monitor CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

USE account_monitor;

-- Settings table for configuration
CREATE TABLE IF NOT EXISTS settings (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100) UNIQUE NOT NULL,
    value TEXT,
    description TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);

-- Networks table
CREATE TABLE IF NOT EXISTS networks (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100) UNIQUE NOT NULL,
    display_name VARCHAR(100),
    network_type ENUM('substrate', 'substrate-evm') DEFAULT 'substrate',
    rpc_url VARCHAR(255) NOT NULL,
    ws_url VARCHAR(255),
    decimals TINYINT UNSIGNED DEFAULT 10,
    symbol VARCHAR(20),
    ss58_prefix SMALLINT UNSIGNED DEFAULT 42,
    active BOOLEAN DEFAULT TRUE,
    last_checked_block BIGINT UNSIGNED DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_active (active),
    INDEX idx_network_type (network_type)
);

-- Network pallets detection
CREATE TABLE IF NOT EXISTS network_pallets (
    id INT AUTO_INCREMENT PRIMARY KEY,
    network_id INT NOT NULL,
    pallet_name VARCHAR(100) NOT NULL,
    pallet_index INT,
    detected BOOLEAN DEFAULT FALSE,
    metadata JSON,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    UNIQUE KEY unique_network_pallet (network_id, pallet_name),
    INDEX idx_network_pallet (network_id, pallet_name)
);

-- Accounts table
CREATE TABLE IF NOT EXISTS accounts (
    id INT AUTO_INCREMENT PRIMARY KEY,
    address VARCHAR(255) UNIQUE NOT NULL,
    address_type ENUM('substrate', 'evm') DEFAULT 'substrate',
    name VARCHAR(100),
    description TEXT,
    monitor_enabled BOOLEAN DEFAULT TRUE,
    discord_notify BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_monitor_enabled (monitor_enabled),
    INDEX idx_address_type (address_type)
);

-- Network tokens (native + assets)
CREATE TABLE IF NOT EXISTS network_tokens (
    id INT AUTO_INCREMENT PRIMARY KEY,
    network_id INT NOT NULL,
    token_type ENUM('native', 'asset', 'foreign_asset') DEFAULT 'native',
    token_id VARCHAR(100),
    symbol VARCHAR(100),
    name VARCHAR(255),
    decimals TINYINT UNSIGNED DEFAULT 10,
    pallet_name VARCHAR(100),
    metadata JSON,
    active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    UNIQUE KEY unique_network_token (network_id, token_type, token_id),
    INDEX idx_network_token (network_id, token_type),
    INDEX idx_token_active (active)
);

-- Bounties table
CREATE TABLE IF NOT EXISTS bounties (
    id INT AUTO_INCREMENT PRIMARY KEY,
    network_id INT NOT NULL,
    bounty_id BIGINT UNSIGNED NOT NULL,
    proposer VARCHAR(255),
    curator VARCHAR(255),
    fee VARCHAR(100),
    curator_deposit VARCHAR(100),
    bond VARCHAR(100),
    value VARCHAR(100),
    status VARCHAR(50),
    description TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    UNIQUE KEY unique_network_bounty (network_id, bounty_id),
    INDEX idx_status (status),
    INDEX idx_curator (curator)
);

-- Child bounties table
CREATE TABLE IF NOT EXISTS child_bounties (
    id INT AUTO_INCREMENT PRIMARY KEY,
    bounty_id INT NOT NULL,
    child_bounty_id BIGINT UNSIGNED NOT NULL,
    network_token_id INT NOT NULL,
    curator_address VARCHAR(255),
    beneficiary_address VARCHAR(255),
    value VARCHAR(100),
    fee VARCHAR(100),
    status ENUM('added', 'curator_proposed', 'active', 'pending_award', 'awarded', 'claimed') DEFAULT 'added',
    description TEXT,
    awarded_at DATETIME,
    claimed_at DATETIME,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (bounty_id) REFERENCES bounties(id) ON DELETE CASCADE,
    FOREIGN KEY (network_token_id) REFERENCES network_tokens(id) ON DELETE CASCADE,
    UNIQUE KEY unique_child_bounty (bounty_id, child_bounty_id),
    INDEX idx_status (status),
    INDEX idx_beneficiary (beneficiary_address),
    INDEX idx_curator (curator_address)
);

-- Balances table
CREATE TABLE IF NOT EXISTS balances (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    account_id INT NOT NULL,
    network_id INT NOT NULL,
    network_token_id INT NOT NULL,
    free VARCHAR(100) DEFAULT '0',
    reserved VARCHAR(100) DEFAULT '0',
    misc_frozen VARCHAR(100) DEFAULT '0',
    fee_frozen VARCHAR(100) DEFAULT '0',
    bonded VARCHAR(100) DEFAULT '0',
    total VARCHAR(100) DEFAULT '0',
    last_updated TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    FOREIGN KEY (network_token_id) REFERENCES network_tokens(id) ON DELETE CASCADE,
    UNIQUE KEY unique_account_network_token (account_id, network_id, network_token_id),
    INDEX idx_account_network (account_id, network_id),
    INDEX idx_last_updated (last_updated)
);

-- Balance history for tracking changes
CREATE TABLE IF NOT EXISTS balance_history (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    balance_id BIGINT NOT NULL,
    account_id INT NOT NULL,
    network_id INT NOT NULL,
    network_token_id INT NOT NULL,
    free_before VARCHAR(100),
    free_after VARCHAR(100),
    total_before VARCHAR(100),
    total_after VARCHAR(100),
    change_amount VARCHAR(100),
    change_type ENUM('increase', 'decrease', 'no_change') DEFAULT 'no_change',
    tx_hash VARCHAR(100),
    block_number BIGINT UNSIGNED,
    recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (balance_id) REFERENCES balances(id) ON DELETE CASCADE,
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    FOREIGN KEY (network_token_id) REFERENCES network_tokens(id) ON DELETE CASCADE,
    INDEX idx_account_time (account_id, recorded_at),
    INDEX idx_change_type (change_type),
    INDEX idx_block_number (block_number)
);

-- Account roles (validator, nominator, collator)
CREATE TABLE IF NOT EXISTS account_roles (
    id INT AUTO_INCREMENT PRIMARY KEY,
    account_id INT NOT NULL,
    network_id INT NOT NULL,
    role_type ENUM('validator', 'nominator', 'collator') NOT NULL,
    stash_address VARCHAR(255),
    controller_address VARCHAR(255),
    active BOOLEAN DEFAULT TRUE,
    metadata JSON,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    UNIQUE KEY unique_account_network_role (account_id, network_id, role_type),
    INDEX idx_role_type (role_type),
    INDEX idx_active (active)
);

-- Validator statistics
CREATE TABLE IF NOT EXISTS validator_stats (
    id INT AUTO_INCREMENT PRIMARY KEY,
    account_id INT NOT NULL,
    network_id INT NOT NULL,
    era BIGINT UNSIGNED,
    total_stake VARCHAR(100),
    self_stake VARCHAR(100),
    nominator_count INT DEFAULT 0,
    commission_percent DECIMAL(5,2),
    points INT DEFAULT 0,
    rewards_claimed BOOLEAN DEFAULT FALSE,
    unclaimed_amount VARCHAR(100),
    slash_count INT DEFAULT 0,
    slash_amount VARCHAR(100),
    metadata JSON,
    recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    INDEX idx_account_network_era (account_id, network_id, era),
    INDEX idx_rewards_claimed (rewards_claimed)
);

-- Collator statistics
CREATE TABLE IF NOT EXISTS collator_stats (
    id INT AUTO_INCREMENT PRIMARY KEY,
    account_id INT NOT NULL,
    network_id INT NOT NULL,
    round BIGINT UNSIGNED,
    self_stake VARCHAR(100),
    delegator_count INT DEFAULT 0,
    total_delegation VARCHAR(100),
    commission_percent DECIMAL(5,2),
    blocks_produced INT DEFAULT 0,
    rewards_claimed BOOLEAN DEFAULT FALSE,
    unclaimed_amount VARCHAR(100),
    metadata JSON,
    recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    INDEX idx_account_network_round (account_id, network_id, round),
    INDEX idx_rewards_claimed (rewards_claimed)
);

-- Insert default settings
INSERT INTO settings (name, value, description) VALUES
('discord_webhook', '', 'Discord webhook URL for notifications'),
('discord_token', '', 'Discord bot token'),
('discord_channel_id', '', 'Discord channel ID for notifications'),
('guild_id', '', 'Discord guild/server ID'),
('alerts_channel_id', '', 'Discord channel ID for alerts'),
('summary_channel_id', '', 'Discord channel ID for daily summaries'),
('monitor_role_id', '', 'Discord role ID for monitoring notifications'),
('check_interval_hours', '24', 'Hours between balance checks'),
('validator_check_interval_hours', '8', 'Hours between validator checks'),
('bounty_check_interval_minutes', '30', 'Minutes between bounty checks'),
('enable_notifications', 'true', 'Enable Discord notifications'),
('min_balance_change_notification', '0.0001', 'Minimum balance change for notifications')
ON DUPLICATE KEY UPDATE id=id;

-- Insert default networks
INSERT INTO networks (name, display_name, network_type, rpc_url, ws_url, decimals, symbol, ss58_prefix) VALUES
('polkadot', 'Polkadot', 'substrate', 'https://rpc.polkadot.io', 'wss://rpc.polkadot.io', 10, 'DOT', 0),
('kusama', 'Kusama', 'substrate', 'https://kusama-rpc.polkadot.io', 'wss://kusama-rpc.polkadot.io', 12, 'KSM', 2),
('polkadot-assethub', 'Polkadot Asset Hub', 'substrate', 'https://polkadot-asset-hub-rpc.polkadot.io', 'wss://polkadot-asset-hub-rpc.polkadot.io', 10, 'DOT', 0),
('polkadot-bridgehub', 'Polkadot Bridge Hub', 'substrate', 'https://polkadot-bridge-hub-rpc.polkadot.io', 'wss://polkadot-bridge-hub-rpc.polkadot.io', 10, 'DOT', 0),
('polkadot-collectives', 'Polkadot Collectives', 'substrate', 'https://polkadot-collectives-rpc.polkadot.io', 'wss://polkadot-collectives-rpc.polkadot.io', 10, 'DOT', 0),
('polkadot-coretime', 'Polkadot Coretime', 'substrate', 'https://polkadot-coretime-rpc.polkadot.io', 'wss://polkadot-coretime-rpc.polkadot.io', 10, 'DOT', 0),
('polkadot-people', 'Polkadot People', 'substrate', 'https://polkadot-people-rpc.polkadot.io', 'wss://polkadot-people-rpc.polkadot.io', 10, 'DOT', 0)
ON DUPLICATE KEY UPDATE id=id;

-- Insert native tokens for each network
INSERT INTO network_tokens (network_id, token_type, symbol, name, decimals)
SELECT id, 'native', symbol, display_name, decimals FROM networks
ON DUPLICATE KEY UPDATE id=id;