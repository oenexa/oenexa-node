package core

import (
	"math/big"
	"testing"
)

func TestLedgerBalanceGuard_ValidJournal(t *testing.T) {
	guard := NewLedgerBalanceGuard()

	amount := big.NewInt(1000000000000000000) // 1.0 ONXC
	journal := &DoubleEntryJournal{
		JournalID: "jrnl_test_valid",
		TxHash:    "0xabcd1234",
		Postings: []DoubleEntryPosting{
			{
				AccountCode: "1103", // MPC Hot Vault
				Asset:       "ONXC",
				Direction:   "DEBIT",
				Amount:      amount,
			},
			{
				AccountCode: "2101", // Customer Spot Balance
				Asset:       "ONXC",
				Direction:   "CREDIT",
				Amount:      amount,
			},
		},
	}

	err := guard.ValidateInvariants(journal)
	if err != nil {
		t.Fatalf("expected journal to be valid, got: %v", err)
	}
}

func TestLedgerBalanceGuard_UnbalancedJournal(t *testing.T) {
	guard := NewLedgerBalanceGuard()

	debitAmount := big.NewInt(1000000000000000000) // 1.0 ONXC
	creditAmount := big.NewInt(900000000000000000) // 0.9 ONXC (Missing 0.1)

	journal := &DoubleEntryJournal{
		JournalID: "jrnl_test_unbalanced",
		TxHash:    "0xabcd5678",
		Postings: []DoubleEntryPosting{
			{
				AccountCode: "1103",
				Asset:       "ONXC",
				Direction:   "DEBIT",
				Amount:      debitAmount,
			},
			{
				AccountCode: "2101",
				Asset:       "ONXC",
				Direction:   "CREDIT",
				Amount:      creditAmount,
			},
		},
	}

	err := guard.ValidateInvariants(journal)
	if err == nil {
		t.Fatal("expected error on unbalanced journal, got nil")
	}
}

func TestLedgerBalanceGuard_MultiAssetJournal(t *testing.T) {
	guard := NewLedgerBalanceGuard()

	onxcAmount := big.NewInt(5000000000000000000) // 5 ONXC
	usdtAmount := big.NewInt(150000000)            // 150 USDT

	journal := &DoubleEntryJournal{
		JournalID: "jrnl_test_multi",
		TxHash:    "0xmultitrade",
		Postings: []DoubleEntryPosting{
			{AccountCode: "2101", Asset: "ONXC", Direction: "DEBIT", Amount: onxcAmount},
			{AccountCode: "2101", Asset: "ONXC", Direction: "CREDIT", Amount: onxcAmount},
			{AccountCode: "2101", Asset: "USDT", Direction: "DEBIT", Amount: usdtAmount},
			{AccountCode: "2101", Asset: "USDT", Direction: "CREDIT", Amount: usdtAmount},
		},
	}

	err := guard.ValidateInvariants(journal)
	if err != nil {
		t.Fatalf("expected multi-asset journal to be valid, got: %v", err)
	}
}
