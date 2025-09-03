# Account Monitor for Polkadot/Substrate Networks

A comprehensive monitoring tool for tracking balances, validator/collator rewards, and bounties across Polkadot and Substrate-based networks.

## Features

- **Multi-Network Support**: Monitor accounts across multiple Substrate networks
- **Balance Tracking**: Track native tokens, assets, and foreign assets
- **Validator/Collator Monitoring**: Track rewards, unclaimed eras, and performance
- **Bounty Tracking**: Monitor bounties and child bounties
- **Discord Notifications**: Real-time alerts for balance changes and claimable rewards
- **Automatic Network Discovery**: Detect available pallets and tokens on each network

## Installation

1. Clone the repository
2. Install dependencies: `go mod download`
3. Set up MySQL database and run `docs/sql/database.sql`
4. Configure settings in the database or environment variables
5. Build: `go build -o bin/account-monitor src/account-monitor/main.go`

## Configuration

### Database Settings
Configure these in the `settings` table:
- `discord_webhook_url`: Discord webhook for notifications
- `check_interval_hours`: How often to check balances (default: 24)
- `validator_check_interval_hours`: How often to check validator stats (default: 8)

### Environment Variables
- `MYSQL_DSN`: MySQL connection string
- `DISCORD_WEBHOOK`: Discord webhook URL (optional, overrides DB)

## Usage

### Start the monitor
```bash
./bin/account-monitor
```

### Add networks
Networks are automatically discovered from configuration, or add manually to the database.

### Add accounts to monitor
Add accounts to the `accounts` table:

```sql
INSERT INTO accounts (address, name, monitor_enabled) VALUES ('15oF4uVJwmo4TdGW7VfQxNLavjCXviqxT9S1MgbjMNHr6Sp5', 'Alice', 1);
```

## Architecture

- **Network Manager**: Handles connection to multiple networks
- **Balance Monitor**: Tracks token balances and changes
- **Validator Monitor**: Tracks validator/nominator rewards and performance
- **Collator Monitor**: Tracks collator rewards
- **Bounty Monitor**: Tracks bounties and child bounties
- **Discord Notifier**: Sends alerts to Discord channels

## Database Schema

See `docs/sql/database.sql` for complete schema including:
- Networks and their settings
- Accounts and roles
- Balances and history
- Bounties and child bounties
- Validator/Collator statistics