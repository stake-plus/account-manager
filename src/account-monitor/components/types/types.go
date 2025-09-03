package database

import (
	"database/sql"
	"math/big"
	"time"
)

type Network struct {
	ID               uint
	Name             string
	DisplayName      sql.NullString
	NetworkType      string
	RPCURL           string
	WSURL            sql.NullString
	Decimals         uint8
	Symbol           sql.NullString
	SS58Prefix       uint16
	Active           bool
	LastCheckedBlock uint64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type Account struct {
	ID             uint
	Address        string
	AddressType    string
	Name           sql.NullString
	Description    sql.NullString
	MonitorEnabled bool
	DiscordNotify  bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type NetworkToken struct {
	ID         uint
	NetworkID  uint
	TokenType  string
	TokenID    sql.NullString
	Symbol     string
	Name       sql.NullString
	Decimals   uint8
	PalletName sql.NullString
	Metadata   sql.NullString
	Active     bool
}

type Balance struct {
	ID         uint64
	AccountID  uint
	NetworkID  uint
	TokenID    uint
	Free       *big.Int
	Reserved   *big.Int
	MiscFrozen *big.Int
	FeeFrozen  *big.Int
	Bonded     *big.Int
	Total      *big.Int
}

type BalanceChange struct {
	ID           uint64
	BalanceID    uint64
	AccountID    uint
	NetworkID    uint
	TokenID      uint
	FreeBefore   *big.Int
	FreeAfter    *big.Int
	TotalBefore  *big.Int
	TotalAfter   *big.Int
	ChangeAmount *big.Int
	ChangeType   string
	TxHash       sql.NullString
	BlockNumber  sql.NullInt64
	RecordedAt   time.Time
}

type Bounty struct {
	ID             uint
	NetworkID      uint
	BountyID       uint64
	Proposer       sql.NullString
	Curator        sql.NullString
	Fee            *big.Int
	CuratorDeposit *big.Int
	Bond           *big.Int
	Value          *big.Int
	Status         string
	Description    sql.NullString
}

type ChildBounty struct {
	ID                 uint
	BountyID           uint
	ChildBountyID      uint64
	NetworkTokenID     uint
	CuratorAddress     sql.NullString
	BeneficiaryAddress sql.NullString
	Value              *big.Int
	Fee                *big.Int
	Status             string
	Description        sql.NullString
	AwardedAt          sql.NullTime
	ClaimedAt          sql.NullTime
}

type ValidatorStats struct {
	AccountID              uint
	NetworkID              uint
	TotalStake             *big.Int
	SelfStake              *big.Int
	NominatorCount         uint
	CommissionPercent      float64
	FirstSeenEra           uint
	LastActiveEra          uint
	TotalSlashCount        uint
	TotalSlashAmount       *big.Int
	UnclaimedEras          []uint
	UnclaimedAmount        *big.Int
	ExpiredUnclaimedAmount *big.Int
	TopNominators          []NominatorInfo
}

type NominatorInfo struct {
	Address string   `json:"address"`
	Amount  *big.Int `json:"amount"`
}

type AccountRole struct {
	ID                uint
	AccountID         uint
	NetworkID         uint
	RoleType          string
	StashAddress      sql.NullString
	ControllerAddress sql.NullString
	Active            bool
	Metadata          sql.NullString
}
