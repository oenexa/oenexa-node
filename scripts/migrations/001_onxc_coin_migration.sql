-- ==============================================================================
-- OENEXA ECOSYSTEM DATABASE MIGRATION SCRIPT: 001_onxc_coin_migration.sql
-- PURPOSE:
--   Atomic migration script standardizing the native cryptocurrency from
--   legacy ticker 'OEN' to 'ONXC' (Open Network for the Next Generation Coin).
--
-- AFFECTED DOMAINS:
--   1. Asset Registry & Master Assets
--   2. Token Contracts & Standards
--   3. Supply Management & Historical Emission
--   4. Market Data & Pricing Feeds
--   5. Exchange Listings & Trading Pairs
--   6. Blockchain Network Registry & Chain Metadata
--
-- EXECUTION SAFETY:
--   Executed inside a single atomic transaction. Rolled back automatically on error.
-- ==============================================================================

BEGIN TRANSACTION;

-- 1. Asset Master Record Standardization
UPDATE assets
SET 
    symbol = 'ONXC',
    name = 'Open Network for the Next Generation Coin',
    slug = 'open-network-next-gen-coin',
    description = 'ONXC is the sovereign, post-quantum native cryptocurrency powering the OENEXA Layer-1 high-throughput blockchain.',
    updated_at = NOW()
WHERE symbol = 'OEN';

-- 2. Blockchain Network Metadata Standardization
UPDATE asset_networks
SET 
    native_asset = 'ONXC',
    network_name = 'OENEXA Sovereign L1 Mainnet',
    updated_at = NOW()
WHERE native_asset = 'OEN';

UPDATE network_metadata
SET 
    token_symbol = 'ONXC',
    coin_name = 'Open Network for the Next Generation Coin',
    updated_at = NOW()
WHERE token_symbol = 'OEN';

-- 3. Token Contract Registry
UPDATE asset_contracts
SET 
    native_asset = 'ONXC',
    updated_at = NOW()
WHERE native_asset = 'OEN';

-- 4. Supply Registry & Circulating Emission
UPDATE asset_supply
SET 
    symbol = 'ONXC',
    updated_at = NOW()
WHERE symbol = 'OEN';

-- 5. Market Registry & Real-Time Stats
UPDATE asset_market_stats
SET 
    symbol = 'ONXC',
    pair = REPLACE(pair, 'OEN', 'ONXC'),
    updated_at = NOW()
WHERE symbol = 'OEN' OR pair LIKE '%OEN%';

-- 6. Exchange Listings & Trading Pairs
UPDATE asset_exchange_listings
SET 
    trading_pair = REPLACE(trading_pair, 'OEN', 'ONXC'),
    base_asset = 'ONXC',
    updated_at = NOW()
WHERE base_asset = 'OEN' OR trading_pair LIKE '%OEN%';

-- 7. Wallet Support Matrix
UPDATE asset_wallet_support
SET 
    asset = 'ONXC',
    updated_at = NOW()
WHERE asset = 'OEN';

-- 8. Historical Pricing Feeds
UPDATE asset_prices
SET 
    symbol = 'ONXC',
    pair = REPLACE(pair, 'OEN', 'ONXC')
WHERE symbol = 'OEN' OR pair LIKE '%OEN%';

-- Verification Assertion (Ensure zero unmigrated rows remain)
DO $$
DECLARE
    legacy_count INT;
BEGIN
    SELECT COUNT(*) INTO legacy_count FROM assets WHERE symbol = 'OEN';
    IF legacy_count > 0 THEN
        RAISE EXCEPTION 'Migration failed: % legacy OEN rows still exist in assets table', legacy_count;
    END IF;
END $$;

COMMIT TRANSACTION;
