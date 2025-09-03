package networks

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"sync"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/stake-plus/account-manager/src/account-monitor/components/config"
	"github.com/stake-plus/account-manager/src/account-monitor/components/database"
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

	var network *database.Network
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
				log.Printf("  ✓ Found pallet: %s", palletName)

				// Special handling for Assets pallet
				if palletName == "Assets" {
					m.discoverAssets(api, network.ID)
				}
			}
		}
	}

	return nil
}

func (m *Manager) discoverAssets(api *gsrpc.SubstrateAPI, networkID uint) {
	// This would query the Assets pallet for all available assets
	// and store them in the network_tokens table
	log.Printf("    Discovering assets for network ID %d", networkID)

	// Implementation would involve querying Assets.Asset storage
	// and iterating through all asset IDs
}

func (m *Manager) GetBalance(networkName, address string) (database.Balance, error) {
	api, err := m.getClient(networkName)
	if err != nil {
		return database.Balance{}, err
	}

	// Get metadata first
	meta, err := api.RPC.State.GetMetadataLatest()
	if err != nil {
		return database.Balance{}, err
	}

	// Decode the address
	accountID, err := types.NewAccountIDFromHexString(address)
	if err != nil {
		// Try SS58 decode
		_, err = types.NewAddressFromHexAccountID(address)
		if err != nil {
			return database.Balance{}, fmt.Errorf("invalid address: %s", address)
		}
	}

	// Get account info
	key, err := types.CreateStorageKey(meta, "System", "Account", accountID[:])
	if err != nil {
		return database.Balance{}, err
	}

	var accountInfo types.AccountInfo
	ok, err := api.RPC.State.GetStorageLatest(key, &accountInfo)
	if err != nil || !ok {
		return database.Balance{}, err
	}

	// Convert to our balance type
	balance := database.Balance{
		Free:       accountInfo.Data.Free.Int,
		Reserved:   accountInfo.Data.Reserved.Int,
		MiscFrozen: accountInfo.Data.MiscFrozen.Int,
		FeeFrozen:  big.NewInt(0), // FeeFrozen was removed in newer versions
		Bonded:     big.NewInt(0), // Will be filled from staking pallet
		Total:      new(big.Int).Add(accountInfo.Data.Free.Int, accountInfo.Data.Reserved.Int),
	}

	// Check for staking/bonded balance
	// This would query the Staking pallet for bonded amounts

	return balance, nil
}
