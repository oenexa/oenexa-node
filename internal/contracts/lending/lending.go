package lending

// Position represents a user's CDP (Collateralized Debt Position).
type Position struct {
	CollateralOEN uint64
	DebtQUSD      uint64
}

// OenexaLend implements a simple lending protocol where users can deposit OEN
// as collateral and borrow QUSD against it.
type OenexaLend struct {
	Positions   map[string]*Position
	oraclePrice uint64 // Price of 1 OEN in USD
}

// NewOenexaLend initializes a new lending market.
func NewOenexaLend() *OenexaLend {
	return &OenexaLend{
		Positions:   make(map[string]*Position),
		oraclePrice: 100, // Default price
	}
}

// UpdateOraclePrice updates the oracle price of OEN in USD.
func (m *OenexaLend) UpdateOraclePrice(price uint64) {
	m.oraclePrice = price
}

// DepositCollateral adds OEN collateral to a user's position.
func (m *OenexaLend) DepositCollateral(user string, amountOEN uint64) {
	if m.Positions[user] == nil {
		m.Positions[user] = &Position{}
	}
	m.Positions[user].CollateralOEN += amountOEN
}

// BorrowQUSD attempts to borrow QUSD against the user's OEN collateral.
// Fails if the new Collateralization Ratio (CR) drops below 150%.
func (m *OenexaLend) BorrowQUSD(user string, amountQUSD uint64) bool {
	pos := m.Positions[user]
	if pos == nil {
		return false
	}

	collateralValue := pos.CollateralOEN * m.oraclePrice
	newDebt := pos.DebtQUSD + amountQUSD

	// Require CollateralValue >= 1.5 * newDebt
	// To avoid floating point math: CollateralValue * 100 >= newDebt * 150
	if collateralValue*100 < newDebt*150 {
		return false
	}

	pos.DebtQUSD = newDebt
	return true
}

// Liquidate seizes the collateral if the position's CR drops below 150%.
func (m *OenexaLend) Liquidate(user string) bool {
	pos := m.Positions[user]
	if pos == nil {
		return false
	}
	if pos.DebtQUSD == 0 {
		return false // Nothing to liquidate
	}

	collateralValue := pos.CollateralOEN * m.oraclePrice
	
	// If CR >= 150%, it's healthy
	if collateralValue*100 >= pos.DebtQUSD*150 {
		return false
	}

	// Liquidate by seizing collateral and zeroing out debt
	pos.CollateralOEN = 0
	pos.DebtQUSD = 0

	return true
}
