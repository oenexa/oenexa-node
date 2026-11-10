package rosetta

// Rosetta API v1.4.14 Data Types
// Compliant with Coinbase Rosetta Specification for Institutional Asset Listings

type SubNetworkIdentifier struct {
	Network  string                 `json:"network"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

type NetworkIdentifier struct {
	Blockchain string                `json:"blockchain"`
	Network    string                `json:"network"`
	SubNetwork *SubNetworkIdentifier `json:"sub_network_identifier,omitempty"`
}

type NetworkListResponse struct {
	NetworkIdentifiers []*NetworkIdentifier `json:"network_identifiers"`
}

type BlockIdentifier struct {
	Index int64  `json:"index"`
	Hash  string `json:"hash"`
}

type PartialBlockIdentifier struct {
	Index *int64  `json:"index,omitempty"`
	Hash  *string `json:"hash,omitempty"`
}

type Peer struct {
	PeerID   string                 `json:"peer_id"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

type SyncStatus struct {
	CurrentIndex *int64 `json:"current_index,omitempty"`
	TargetIndex  *int64 `json:"target_index,omitempty"`
	Stage        *string `json:"stage,omitempty"`
	Synced       *bool  `json:"synced,omitempty"`
}

type NetworkStatusResponse struct {
	CurrentBlockIdentifier BlockIdentifier   `json:"current_block_identifier"`
	CurrentBlockTimestamp  int64             `json:"current_block_timestamp"`
	GenesisBlockIdentifier BlockIdentifier   `json:"genesis_block_identifier"`
	OldestBlockIdentifier  *BlockIdentifier  `json:"oldest_block_identifier,omitempty"`
	SyncStatus             *SyncStatus       `json:"sync_status,omitempty"`
	Peers                  []*Peer           `json:"peers"`
}

type OperationStatus struct {
	Status     string `json:"status"`
	Successful bool   `json:"successful"`
}

type AllowConfiguration struct {
	OperationStatuses       []*OperationStatus `json:"operation_statuses"`
	OperationTypes          []string           `json:"operation_types"`
	Errors                  []*Error           `json:"errors"`
	HistoricalBalanceLookup bool               `json:"historical_balance_lookup"`
	CallMethods             []string           `json:"call_methods,omitempty"`
	BalanceExemptions       []interface{}      `json:"balance_exemptions,omitempty"`
	MempoolCoins            bool               `json:"mempool_coins"`
}

type Version struct {
	RosettaVersion    string  `json:"rosetta_version"`
	NodeVersion       string  `json:"node_version"`
	MiddlewareVersion *string `json:"middleware_version,omitempty"`
}

type NetworkOptionsResponse struct {
	Version Version            `json:"version"`
	Allow   AllowConfiguration `json:"allow"`
}

type SubAccountIdentifier struct {
	Address  string                 `json:"address"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

type AccountIdentifier struct {
	Address    string                `json:"address"`
	SubAccount *SubAccountIdentifier `json:"sub_account,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

type Currency struct {
	Symbol   string                 `json:"symbol"`
	Decimals int32                  `json:"decimals"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

type Amount struct {
	Value    string                 `json:"value"`
	Currency Currency               `json:"currency"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

type AccountBalanceRequest struct {
	NetworkIdentifier NetworkIdentifier       `json:"network_identifier"`
	AccountIdentifier AccountIdentifier       `json:"account_identifier"`
	BlockIdentifier   *PartialBlockIdentifier `json:"block_identifier,omitempty"`
	Currencies        []*Currency             `json:"currencies,omitempty"`
}

type AccountBalanceResponse struct {
	BlockIdentifier BlockIdentifier        `json:"block_identifier"`
	Balances        []*Amount              `json:"balances"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

type OperationIdentifier struct {
	Index        int64  `json:"index"`
	NetworkIndex *int64 `json:"network_index,omitempty"`
}

type Operation struct {
	OperationIdentifier OperationIdentifier   `json:"operation_identifier"`
	RelatedOperations   []OperationIdentifier `json:"related_operations,omitempty"`
	Type                string                `json:"type"`
	Status              *string               `json:"status,omitempty"`
	Account             *AccountIdentifier    `json:"account,omitempty"`
	Amount              *Amount               `json:"amount,omitempty"`
	Metadata            map[string]interface{}`json:"metadata,omitempty"`
}

type TransactionIdentifier struct {
	Hash string `json:"hash"`
}

type RelatedTransaction struct {
	NetworkIdentifier     *NetworkIdentifier    `json:"network_identifier,omitempty"`
	TransactionIdentifier TransactionIdentifier `json:"transaction_identifier"`
	Direction             string                `json:"direction"`
}

type Transaction struct {
	TransactionIdentifier TransactionIdentifier  `json:"transaction_identifier"`
	Operations            []*Operation           `json:"operations"`
	RelatedTransactions   []RelatedTransaction   `json:"related_transactions,omitempty"`
	Metadata              map[string]interface{} `json:"metadata,omitempty"`
}

type Block struct {
	BlockIdentifier       BlockIdentifier        `json:"block_identifier"`
	ParentBlockIdentifier BlockIdentifier        `json:"parent_block_identifier"`
	Timestamp             int64                  `json:"timestamp"`
	Transactions          []*Transaction         `json:"transactions"`
	Metadata              map[string]interface{} `json:"metadata,omitempty"`
}

type BlockRequest struct {
	NetworkIdentifier NetworkIdentifier      `json:"network_identifier"`
	BlockIdentifier   PartialBlockIdentifier `json:"block_identifier"`
}

type BlockResponse struct {
	Block             *Block                 `json:"block,omitempty"`
	OtherTransactions []TransactionIdentifier `json:"other_transactions,omitempty"`
}

type BlockTransactionRequest struct {
	NetworkIdentifier     NetworkIdentifier     `json:"network_identifier"`
	BlockIdentifier       BlockIdentifier       `json:"block_identifier"`
	TransactionIdentifier TransactionIdentifier `json:"transaction_identifier"`
}

type BlockTransactionResponse struct {
	Transaction *Transaction `json:"transaction"`
}

type MempoolResponse struct {
	TransactionIdentifiers []*TransactionIdentifier `json:"transaction_identifiers"`
}

type MempoolTransactionRequest struct {
	NetworkIdentifier     NetworkIdentifier     `json:"network_identifier"`
	TransactionIdentifier TransactionIdentifier `json:"transaction_identifier"`
}

type MempoolTransactionResponse struct {
	Transaction *Transaction `json:"transaction"`
}

// Construction API types
type PublicKey struct {
	HexBytes  string `json:"hex_bytes"`
	CurveType string `json:"curve_type"` // "dilithium" (ML-DSA-65)
}

type ConstructionDeriveRequest struct {
	NetworkIdentifier NetworkIdentifier `json:"network_identifier"`
	PublicKey         PublicKey         `json:"public_key"`
}

type ConstructionDeriveResponse struct {
	AccountIdentifier *AccountIdentifier     `json:"account_identifier"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
}

type ConstructionPreprocessRequest struct {
	NetworkIdentifier NetworkIdentifier      `json:"network_identifier"`
	Operations        []*Operation           `json:"operations"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
}

type ConstructionPreprocessResponse struct {
	Options            map[string]interface{} `json:"options,omitempty"`
	RequiredPublicKeys []*AccountIdentifier   `json:"required_public_keys,omitempty"`
}

type ConstructionMetadataRequest struct {
	NetworkIdentifier NetworkIdentifier      `json:"network_identifier"`
	Options            map[string]interface{} `json:"options"`
}

type ConstructionMetadataResponse struct {
	Metadata     map[string]interface{} `json:"metadata"`
	SuggestedFee []*Amount              `json:"suggested_fee,omitempty"`
}

type SigningPayload struct {
	AccountIdentifier *AccountIdentifier `json:"account_identifier,omitempty"`
	HexBytes          string             `json:"hex_bytes"`
	SignatureType     string             `json:"signature_type,omitempty"`
}

type ConstructionPayloadsRequest struct {
	NetworkIdentifier NetworkIdentifier      `json:"network_identifier"`
	Operations        []*Operation           `json:"operations"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
}

type ConstructionPayloadsResponse struct {
	UnsignedTransaction string            `json:"unsigned_transaction"`
	Payloads            []*SigningPayload `json:"payloads"`
}

type Signature struct {
	SigningPayload SigningPayload `json:"signing_payload"`
	PublicKey      PublicKey      `json:"public_key"`
	SignatureType  string         `json:"signature_type"`
	HexBytes       string         `json:"hex_bytes"`
}

type ConstructionCombineRequest struct {
	NetworkIdentifier   NetworkIdentifier `json:"network_identifier"`
	UnsignedTransaction string            `json:"unsigned_transaction"`
	Signatures          []*Signature      `json:"signatures"`
}

type ConstructionCombineResponse struct {
	SignedTransaction string `json:"signed_transaction"`
}

type ConstructionHashRequest struct {
	NetworkIdentifier NetworkIdentifier `json:"network_identifier"`
	SignedTransaction string            `json:"signed_transaction"`
}

type ConstructionHashResponse struct {
	TransactionIdentifier TransactionIdentifier `json:"transaction_identifier"`
}

type ConstructionParseRequest struct {
	NetworkIdentifier NetworkIdentifier `json:"network_identifier"`
	Signed            bool              `json:"signed"`
	Transaction       string            `json:"transaction"`
}

type ConstructionParseResponse struct {
	Operations               []*Operation           `json:"operations"`
	AccountIdentifierSigners []*AccountIdentifier   `json:"account_identifier_signers,omitempty"`
	Metadata                 map[string]interface{} `json:"metadata,omitempty"`
}

type ConstructionSubmitRequest struct {
	NetworkIdentifier NetworkIdentifier `json:"network_identifier"`
	SignedTransaction string            `json:"signed_transaction"`
}

type ConstructionSubmitResponse struct {
	TransactionIdentifier TransactionIdentifier  `json:"transaction_identifier"`
	Metadata              map[string]interface{} `json:"metadata,omitempty"`
}

// Error represents standard Rosetta error format
type Error struct {
	Code        int32                  `json:"code"`
	Message     string                 `json:"message"`
	Description *string                `json:"description,omitempty"`
	Retriable   bool                   `json:"retriable"`
	Details     map[string]interface{} `json:"details,omitempty"`
}

var (
	ErrInvalidNetwork = &Error{
		Code:      1,
		Message:   "Invalid network identifier",
		Retriable: false,
	}
	ErrBlockNotFound = &Error{
		Code:      2,
		Message:   "Block not found",
		Retriable: false,
	}
	ErrTxNotFound = &Error{
		Code:      3,
		Message:   "Transaction not found",
		Retriable: false,
	}
	ErrInvalidAccount = &Error{
		Code:      4,
		Message:   "Invalid account address",
		Retriable: false,
	}
	ErrInvalidPayload = &Error{
		Code:      5,
		Message:   "Invalid transaction payload",
		Retriable: false,
	}
	ErrBroadcastFailed = &Error{
		Code:      6,
		Message:   "Failed to submit transaction to mempool",
		Retriable: true,
	}
	ErrInternal = &Error{
		Code:      500,
		Message:   "Internal node error",
		Retriable: true,
	}
)
