-- ==============================================================================
-- OENEXA ECOSYSTEM DATABASE ROLLBACK SCRIPT: 001_onxc_coin_rollback.sql
-- PURPOSE:
--   Deterministic, zero-data-loss rollback script restoring 'ONXC' records
--   back to legacy 'OEN' state if an emergency ecosystem rollback is required.
-- ==============================================================================

BEGIN TRANSACTION;

-- 1. Asset Master Record Rollback
UPDATE assets
SET 
    symbol = 'OEN',
    name = 'Oenexa Native Coin',
    slug = 'oenexa-native-coin',
    description = 'Native cryptocurrency of the OENEXA Layer-1 sovereign network.',
    updated_at = NOW()
WHERE symbol = 'ONXC';

-- 2. Blockchain Network Metadata Rollback
UPDATE asset_networks
SET 
    native_asset = 'OEN',
    updated_at = NOW()
WHERE native_asset = 'ONXC';

UPDATE network_metadata
SET 
    token_symbol = 'OEN',
    coin_name = 'Oenexa Native Coin',
    updated_at = NOW()
WHERE token_symbol = 'ONXC';

-- 3. Token Contract Registry Rollback
UPDATE asset_contracts
SET 
    native_asset = 'OEN',
    updated_at = NOW()
WHERE native_asset = 'ONXC';

-- 4. Supply Registry Rollback
UPDATE asset_supply
SET 
    symbol = 'OEN',
    updated_at = NOW()
WHERE symbol = 'ONXC';

-- 5. Market Registry Rollback
UPDATE asset_market_stats
SET 
    symbol = 'OEN',
    pair = REPLACE(pair, 'ONXC', 'OEN'),
    updated_at = NOW()
WHERE symbol = 'ONXC' OR pair LIKE '%ONXC%';

-- 6. Exchange Listings Rollback
UPDATE asset_exchange_listings
SET 
    trading_pair = REPLACE(trading_pair, 'ONXC', 'OEN'),
    base_asset = 'OEN',
    updated_at = NOW()
WHERE base_asset = 'ONXC' OR trading_pair LIKE '%ONXC%';

-- 7. Wallet Support Matrix Rollback
UPDATE asset_wallet_support
SET 
    asset = 'OEN',
    updated_at = NOW()
WHERE asset = 'ONXC';

-- 8. Historical Pricing Feeds Rollback
UPDATE asset_prices
SET 
    symbol = 'OEN',
    pair = REPLACE(pair, 'ONXC', 'OEN')
WHERE symbol = 'ONXC' OR pair LIKE '%ONXC%';

COMMIT TRANSACTION;
