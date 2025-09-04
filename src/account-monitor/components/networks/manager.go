package networks

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"math/big"
	"strconv"
	"strings"
	"sync"

	"github.com/OneOfOne/xxhash"
	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gstypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/mr-tron/base58"
	"github.com/stake-plus/account-manager/src/account-monitor/components/config"
	"github.com/stake-plus/account-manager/src/account-monitor/components/database"
	types "github.com/stake-plus/account-manager/src/account-monitor/components/types"
)

type Manager struct {
	db      *database.DB
	config  *config.Config
	clients map[string]*gsrpc.SubstrateAPI
	mu      sync.RWMutex
}

func NewManager(db *database.DB, cfg *config.Config) (*Manager, error) {
	return &Manager{
		db:      db,
		config:  cfg,
		clients: make(map[string]*gsrpc.SubstrateAPI),
	}, nil
}

func (m *Manager) getClient(networkName string) (*gsrpc.SubstrateAPI, error) {
	m.mu.RLock()
	client, exists := m.clients[networkName]
	m.mu.RUnlock()

	if exists {
		return client, nil
	}

	// Get network details from database
	networks, err := m.db.GetNetworks()
	if err != nil {
		return nil, err
	}

	var network *types.Network
	for i := range networks {
		if networks[i].Name == networkName {
			network = &networks[i]
			break
		}
	}

	if network == nil {
		return nil, fmt.Errorf("network not found: %s", networkName)
	}

	// Create new client
	url := network.WSURL.String
	if url == "" {
		url = network.RPCURL
	}

	api, err := gsrpc.NewSubstrateAPI(url)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.clients[networkName] = api
	m.mu.Unlock()

	return api, nil
}

func (m *Manager) DiscoverNetworks(ctx context.Context) error {
	networks, err := m.db.GetNetworks()
	if err != nil {
		return err
	}

	for _, network := range networks {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		log.Printf("Discovering pallets for network: %s", network.Name)

		api, err := m.getClient(network.Name)
		if err != nil {
			log.Printf("Failed to connect to %s: %v", network.Name, err)
			continue
		}

		// Get metadata to discover pallets
		meta, err := api.RPC.State.GetMetadataLatest()
		if err != nil {
			log.Printf("Failed to get metadata for %s: %v", network.Name, err)
			continue
		}

		// Check for specific pallets
		pallets := []string{
			"System", "Balances", "Assets", "ForeignAssets",
			"Bounties", "ChildBounties", "Staking", "ParachainStaking",
			"CollatorSelection", "Proxy", "Identity",
		}

		for _, palletName := range pallets {
			hasPallet := false
			for _, module := range meta.AsMetadataV14.Pallets {
				if string(module.Name) == palletName {
					hasPallet = true
					// Store pallet detection
					_, err = m.db.Exec(`
						INSERT INTO network_pallets (network_id, pallet_name, pallet_index, detected)
						VALUES (?, ?, ?, TRUE)
						ON DUPLICATE KEY UPDATE detected = TRUE, pallet_index = VALUES(pallet_index)
					`, network.ID, palletName, module.Index)

					if err != nil {
						log.Printf("Failed to store pallet info: %v", err)
					}
					break
				}
			}

			if hasPallet {
				log.Printf("  ✔ Found pallet: %s", palletName)
				// Special handling for Assets and ForeignAssets pallets
				switch palletName {
				case "Assets":
					m.discoverAssets(api, network.ID, "Assets")
				case "ForeignAssets":
					m.discoverAssets(api, network.ID, "ForeignAssets")
				}
			}
		}
	}

	return nil
}

func (m *Manager) discoverAssets(api *gsrpc.SubstrateAPI, networkID uint, palletName string) {
	log.Printf("    Discovering %s for network ID %d", palletName, networkID)

	_, err := api.RPC.State.GetMetadataLatest()
	if err != nil {
		log.Printf("Failed to get metadata: %v", err)
		return
	}

	// For Assets/ForeignAssets, we need to get all storage keys with the pallet prefix
	// Instead of trying to create a specific storage key, get keys by prefix
	prefix := append(Twox128([]byte(palletName)), Twox128([]byte("Asset"))...)

	keys, err := api.RPC.State.GetKeysLatest(prefix)
	if err != nil {
		log.Printf("Failed to get asset keys: %v", err)
		return
	}

	log.Printf("    Found %d assets in %s", len(keys), palletName)

	tokenType := "asset"
	if palletName == "ForeignAssets" {
		tokenType = "foreign_asset"
	}

	// Process each asset
	for _, key := range keys {
		// The asset ID is encoded in the key after the pallet+storage prefix
		// Skip the pallet hash (16 bytes) + storage hash (16 bytes)
		if len(key) > 32 {
			// Asset ID starts after the hashes
			assetIDBytes := key[32:]

			// Store a placeholder for now - you'd need to query metadata separately
			assetID := fmt.Sprintf("0x%x", assetIDBytes)

			_, err = m.db.Exec(`
				INSERT INTO network_tokens 
				(network_id, token_type, token_id, symbol, name, decimals, pallet_name, active)
				VALUES (?, ?, ?, ?, ?, ?, ?, TRUE)
				ON DUPLICATE KEY UPDATE active = TRUE
			`, networkID, tokenType, assetID,
				fmt.Sprintf("Asset_%s", assetID[:8]), // Temporary symbol
				"Unknown Asset", 10, palletName)

			if err != nil {
				log.Printf("Failed to insert asset: %v", err)
			}
		}
	}
}

// Add helper function
func Twox128(data []byte) []byte {
	h := xxhash.NewS64(0)
	h.Write(data)
	h2 := xxhash.NewS64(1)
	h2.Write(data)

	out := make([]byte, 16)
	binary.LittleEndian.PutUint64(out[0:], h.Sum64())
	binary.LittleEndian.PutUint64(out[8:], h2.Sum64())
	return out
}

// GetAssetBalance gets balance for a specific asset token
func (m *Manager) GetAssetBalance(networkName, address, assetID string) (types.Balance, error) {
	api, err := m.getClient(networkName)
	if err != nil {
		return types.Balance{}, err
	}

	// Get metadata
	meta, err := api.RPC.State.GetMetadataLatest()
	if err != nil {
		return types.Balance{}, err
	}

	// Decode address to AccountID
	var accountID gstypes.AccountID
	if strings.HasPrefix(address, "0x") {
		err = codec.DecodeFromHex(address, &accountID)
	} else {
		accountID, err = decodeSS58Address(address)
	}
	if err != nil {
		return types.Balance{}, err
	}

	// Convert assetID string to proper format
	// Asset IDs can be numeric or hex encoded
	var assetIDBytes []byte
	if strings.HasPrefix(assetID, "0x") {
		// Hex encoded
		assetIDBytes, err = codec.HexDecodeString(assetID)
		if err != nil {
			return types.Balance{}, fmt.Errorf("failed to decode asset ID: %w", err)
		}
	} else {
		// Try as numeric ID (u32 or u128)
		// For now, assume u32
		assetIDNum, err := strconv.ParseUint(assetID, 10, 32)
		if err != nil {
			return types.Balance{}, fmt.Errorf("failed to parse asset ID: %w", err)
		}
		assetIDBytes = make([]byte, 4)
		binary.LittleEndian.PutUint32(assetIDBytes, uint32(assetIDNum))
	}

	// Try Assets pallet first
	key, err := gstypes.CreateStorageKey(meta, "Assets", "Account", assetIDBytes, accountID[:])
	if err == nil {
		var assetBalance struct {
			Balance  gstypes.U128
			IsFrozen bool
			Reason   []byte
		}

		ok, err := api.RPC.State.GetStorageLatest(key, &assetBalance)
		if err == nil && ok {
			return types.Balance{
				Free:       assetBalance.Balance.Int,
				Reserved:   big.NewInt(0),
				MiscFrozen: big.NewInt(0),
				FeeFrozen:  big.NewInt(0),
				Bonded:     big.NewInt(0),
				Total:      assetBalance.Balance.Int,
			}, nil
		}
	}

	// Try ForeignAssets pallet
	key, err = gstypes.CreateStorageKey(meta, "ForeignAssets", "Account", assetIDBytes, accountID[:])
	if err == nil {
		var assetBalance struct {
			Balance  gstypes.U128
			IsFrozen bool
			Reason   []byte
		}

		ok, err := api.RPC.State.GetStorageLatest(key, &assetBalance)
		if err == nil && ok {
			return types.Balance{
				Free:       assetBalance.Balance.Int,
				Reserved:   big.NewInt(0),
				MiscFrozen: big.NewInt(0),
				FeeFrozen:  big.NewInt(0),
				Bonded:     big.NewInt(0),
				Total:      assetBalance.Balance.Int,
			}, nil
		}
	}

	// Return zero balance if not found
	return types.Balance{
		Free:       big.NewInt(0),
		Reserved:   big.NewInt(0),
		MiscFrozen: big.NewInt(0),
		FeeFrozen:  big.NewInt(0),
		Bonded:     big.NewInt(0),
		Total:      big.NewInt(0),
	}, nil
}

// decodeSS58Address decodes an SS58 address to AccountID
func decodeSS58Address(address string) (gstypes.AccountID, error) {
	// Decode base58
	decoded, err := base58.Decode(address)
	if err != nil {
		return gstypes.AccountID{}, fmt.Errorf("base58 decode failed: %w", err)
	}

	// SS58 addresses have the following structure:
	// [prefix][publicKey][checksum]
	// For addresses with prefix < 64: 1 byte prefix + 32 bytes pubkey + 2 bytes checksum = 35 bytes
	// For addresses with prefix >= 64: 2 byte prefix + 32 bytes pubkey + 2 bytes checksum = 36 bytes

	var pubkeyStart int
	if len(decoded) == 35 {
		// Single byte prefix
		pubkeyStart = 1
	} else if len(decoded) == 36 {
		// Two byte prefix
		pubkeyStart = 2
	} else {
		return gstypes.AccountID{}, fmt.Errorf("invalid address length: %d", len(decoded))
	}

	// Extract the public key (32 bytes)
	if len(decoded) < pubkeyStart+32 {
		return gstypes.AccountID{}, fmt.Errorf("address too short for public key")
	}

	var accountID gstypes.AccountID
	copy(accountID[:], decoded[pubkeyStart:pubkeyStart+32])

	return accountID, nil
}

func (m *Manager) GetBalance(networkName, addressStr string) (types.Balance, error) {
	api, err := m.getClient(networkName)
	if err != nil {
		return types.Balance{}, err
	}

	// Get metadata
	meta, err := api.RPC.State.GetMetadataLatest()
	if err != nil {
		return types.Balance{}, err
	}

	// Handle address conversion
	var accountID gstypes.AccountID

	// Remove whitespace
	addressStr = strings.TrimSpace(addressStr)

	// If it starts with 0x, it's already hex
	if strings.HasPrefix(addressStr, "0x") {
		err = codec.DecodeFromHex(addressStr, &accountID)
		if err != nil {
			return types.Balance{}, fmt.Errorf("failed to decode hex address: %w", err)
		}
	} else if len(addressStr) == 64 {
		// It might be hex without 0x prefix (64 chars = 32 bytes)
		accountIDPtr, err := gstypes.NewAccountIDFromHexString(addressStr)
		if err != nil {
			return types.Balance{}, fmt.Errorf("failed to decode hex string: %w", err)
		}
		accountID = *accountIDPtr
	} else {
		// Try SS58 decode
		accountID, err = decodeSS58Address(addressStr)
		if err != nil {
			return types.Balance{}, fmt.Errorf("failed to decode SS58 address %s: %w", addressStr, err)
		}
	}

	// Get account info
	key, err := gstypes.CreateStorageKey(meta, "System", "Account", accountID[:])
	if err != nil {
		return types.Balance{}, err
	}

	var accountInfo gstypes.AccountInfo
	ok, err := api.RPC.State.GetStorageLatest(key, &accountInfo)
	if err != nil {
		return types.Balance{}, err
	}

	if !ok {
		// Account doesn't exist on this network, return zero balance
		return types.Balance{
			Free:       big.NewInt(0),
			Reserved:   big.NewInt(0),
			MiscFrozen: big.NewInt(0),
			FeeFrozen:  big.NewInt(0),
			Bonded:     big.NewInt(0),
			Total:      big.NewInt(0),
		}, nil
	}

	// Convert to our balance type
	balance := types.Balance{
		Free:       accountInfo.Data.Free.Int,
		Reserved:   accountInfo.Data.Reserved.Int,
		MiscFrozen: accountInfo.Data.MiscFrozen.Int,
		FeeFrozen:  big.NewInt(0), // FeeFrozen was removed in newer versions
		Bonded:     big.NewInt(0), // Will be filled from staking pallet
		Total:      new(big.Int).Add(accountInfo.Data.Free.Int, accountInfo.Data.Reserved.Int),
	}

	// Check for staking/bonded balance if Staking pallet exists
	// This would query the Staking pallet for bonded amounts

	return balance, nil
}
