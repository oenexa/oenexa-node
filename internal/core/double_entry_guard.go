package core

import (
	"errors"
	"fmt"
	"math/big"
)

// DoubleEntryPosting represents a single immutable leg of a transaction.
type DoubleEntryPosting struct {
	AccountCode string   // e.g. "1103" (Hot Vault), "2101" (Client Spot), "4101" (Fee)
	Asset       string   // e.g. "ONXC"
	Direction   string   // "DEBIT" or "CREDIT"
	Amount      *big.Int // Fixed-point atomic units (18 decimals for ONXC)
}

// DoubleEntryJournal represents an atomic batch of postings that must sum to zero.
type DoubleEntryJournal struct {
	JournalID string
	TxHash    string
	Postings  []DoubleEntryPosting
}

// LedgerBalanceGuard enforces banking-grade double-entry invariants at consensus level.
type LedgerBalanceGuard struct{}

// NewLedgerBalanceGuard creates a new validator instance.
func NewLedgerBalanceGuard() *LedgerBalanceGuard {
	return &LedgerBalanceGuard{}
}

// ValidateInvariants verifies that sum(Debits) == sum(Credits) for each asset in the journal.
func (g *LedgerBalanceGuard) ValidateInvariants(journal *DoubleEntryJournal) error {
	if journal == nil || len(journal.Postings) == 0 {
		return errors.New("empty double-entry journal")
	}

	debitsByAsset := make(map[string]*big.Int)
	creditsByAsset := make(map[string]*big.Int)

	for _, p := range journal.Postings {
		if p.Amount == nil || p.Amount.Sign() <= 0 {
			return fmt.Errorf("invalid non-positive posting amount in account %s", p.AccountCode)
		}

		switch p.Direction {
		case "DEBIT":
			if _, exists := debitsByAsset[p.Asset]; !exists {
				debitsByAsset[p.Asset] = new(big.Int)
			}
			debitsByAsset[p.Asset].Add(debitsByAsset[p.Asset], p.Amount)
		case "CREDIT":
			if _, exists := creditsByAsset[p.Asset]; !exists {
				creditsByAsset[p.Asset] = new(big.Int)
			}
			creditsByAsset[p.Asset].Add(creditsByAsset[p.Asset], p.Amount)
		default:
			return fmt.Errorf("invalid posting direction %s", p.Direction)
		}
	}

	// Verify Debits == Credits across every asset represented
	for asset, debitTotal := range debitsByAsset {
		creditTotal, exists := creditsByAsset[asset]
		if !exists || debitTotal.Cmp(creditTotal) != 0 {
			credStr := "0"
			if exists {
				credStr = creditTotal.String()
			}
			return fmt.Errorf("double-entry invariant violation for asset %s: Debits (%s) != Credits (%s)",
				asset, debitTotal.String(), credStr)
		}
	}

	for asset, creditTotal := range creditsByAsset {
		if _, exists := debitsByAsset[asset]; !exists {
			return fmt.Errorf("double-entry invariant violation for asset %s: Missing debits for credits (%s)",
				asset, creditTotal.String())
		}
	}

	return nil
}
