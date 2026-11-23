package core

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"

	"github.com/oenexa/oenexa/internal/crypto"
)

var (
	// ErrNegativeBalanceProhibited is returned when any client liability is negative.
	ErrNegativeBalanceProhibited = errors.New("solvency: negative client balance is strictly prohibited")
	// ErrLeafNotFound is returned when an account proof is requested for a nonexistent leaf.
	ErrLeafNotFound = errors.New("solvency: client leaf not found in tree")
	// ErrInvalidProof is returned when a Merkle sum proof fails cryptographic validation.
	ErrInvalidProof = errors.New("solvency: invalid proof path or sum mismatch")
)

// ClientLiabilityLeaf represents a verifiable client deposit record.
type ClientLiabilityLeaf struct {
	AccountID string
	Balance   *big.Int
}

// SumTreeNode holds both the cryptographic hash and aggregated subtree liability sum.
type SumTreeNode struct {
	Hash [crypto.HashSize]byte
	Sum  *big.Int
}

// SolvencySumProof allows an individual client to cryptographically prove that
// their positive balance is included in the published exchange liability root.
type SolvencySumProof struct {
	AccountID string
	Balance   *big.Int
	Siblings  []SumTreeNode
	IsLeft    []bool // true if the sibling is to the left of the current node
}

// SolvencySumTree is a specialized binary Merkle tree with aggregate sums for Proof of Solvency.
type SolvencySumTree struct {
	Root   *SumTreeNode
	leaves []ClientLiabilityLeaf
	layers [][]*SumTreeNode
}

// HashLiabilityLeaf computes the domain-separated cryptographic digest of a client balance leaf.
func HashLiabilityLeaf(accountID string, balance *big.Int) [crypto.HashSize]byte {
	prefix := []byte("OENEXA_SOLVENCY_LEAF_V1:")
	payload := append(prefix, []byte(accountID)...)
	payload = append(payload, balance.Bytes()...)
	return crypto.Hash256(payload)
}

// HashSumNodes computes the parent node digest combining left and right subtrees with their liabilities.
func HashSumNodes(leftHash [crypto.HashSize]byte, leftSum *big.Int, rightHash [crypto.HashSize]byte, rightSum *big.Int) [crypto.HashSize]byte {
	var buf bytes.Buffer
	buf.WriteString("OENEXA_SOLVENCY_NODE_V1:")
	buf.Write(leftHash[:])
	buf.Write(leftSum.Bytes())
	buf.Write(rightHash[:])
	buf.Write(rightSum.Bytes())
	return crypto.Hash256(buf.Bytes())
}

// NewSolvencySumTree constructs a Merkle Sum Tree over the provided client liability leaves.
func NewSolvencySumTree(leaves []ClientLiabilityLeaf) (*SolvencySumTree, error) {
	if len(leaves) == 0 {
		return &SolvencySumTree{
			Root: &SumTreeNode{
				Hash: EmptyMerkleRoot(),
				Sum:  big.NewInt(0),
			},
			leaves: nil,
			layers: nil,
		}, nil
	}

	leafNodes := make([]*SumTreeNode, len(leaves))
	for i, leaf := range leaves {
		if leaf.Balance == nil || leaf.Balance.Sign() < 0 {
			return nil, fmt.Errorf("%w for account %s", ErrNegativeBalanceProhibited, leaf.AccountID)
		}
		leafNodes[i] = &SumTreeNode{
			Hash: HashLiabilityLeaf(leaf.AccountID, leaf.Balance),
			Sum:  new(big.Int).Set(leaf.Balance),
		}
	}

	layers := [][]*SumTreeNode{leafNodes}
	current := leafNodes

	for len(current) > 1 {
		var next []*SumTreeNode
		for i := 0; i < len(current); i += 2 {
			if i+1 < len(current) {
				left := current[i]
				right := current[i+1]
				parentSum := new(big.Int).Add(left.Sum, right.Sum)
				parentHash := HashSumNodes(left.Hash, left.Sum, right.Hash, right.Sum)
				next = append(next, &SumTreeNode{Hash: parentHash, Sum: parentSum})
			} else {
				// Duplicate solitary node for odd length to maintain balanced binary tree
				left := current[i]
				parentSum := new(big.Int).Add(left.Sum, left.Sum)
				parentHash := HashSumNodes(left.Hash, left.Sum, left.Hash, left.Sum)
				next = append(next, &SumTreeNode{Hash: parentHash, Sum: parentSum})
			}
		}
		layers = append(layers, next)
		current = next
	}

	return &SolvencySumTree{
		Root:   current[0],
		leaves: leaves,
		layers: layers,
	}, nil
}

// GenerateProof creates a Merkle inclusion proof for a specified account ID.
func (tree *SolvencySumTree) GenerateProof(accountID string) (*SolvencySumProof, error) {
	idx := -1
	for i, leaf := range tree.leaves {
		if leaf.AccountID == accountID {
			idx = i
			break
		}
	}
	if idx == -1 {
		return nil, fmt.Errorf("%w: %s", ErrLeafNotFound, accountID)
	}

	proof := &SolvencySumProof{
		AccountID: tree.leaves[idx].AccountID,
		Balance:   new(big.Int).Set(tree.leaves[idx].Balance),
		Siblings:  make([]SumTreeNode, 0, len(tree.layers)-1),
		IsLeft:    make([]bool, 0, len(tree.layers)-1),
	}

	currIdx := idx
	for layerIdx := 0; layerIdx < len(tree.layers)-1; layerIdx++ {
		layer := tree.layers[layerIdx]
		var siblingIdx int
		var isLeft bool

		if currIdx%2 == 0 {
			// Sibling is on right
			if currIdx+1 < len(layer) {
				siblingIdx = currIdx + 1
			} else {
				siblingIdx = currIdx // duplicated odd node
			}
			isLeft = false
		} else {
			// Sibling is on left
			siblingIdx = currIdx - 1
			isLeft = true
		}

		siblingNode := layer[siblingIdx]
		proof.Siblings = append(proof.Siblings, SumTreeNode{
			Hash: siblingNode.Hash,
			Sum:  new(big.Int).Set(siblingNode.Sum),
		})
		proof.IsLeft = append(proof.IsLeft, isLeft)
		currIdx = currIdx / 2
	}

	return proof, nil
}

// VerifySolvencyProof checks if a given inclusion proof matches the published root.
func VerifySolvencyProof(root *SumTreeNode, proof *SolvencySumProof) (bool, error) {
	if root == nil || proof == nil {
		return false, ErrInvalidProof
	}
	if proof.Balance == nil || proof.Balance.Sign() < 0 {
		return false, ErrNegativeBalanceProhibited
	}

	currHash := HashLiabilityLeaf(proof.AccountID, proof.Balance)
	currSum := new(big.Int).Set(proof.Balance)

	for i := 0; i < len(proof.Siblings); i++ {
		sibling := proof.Siblings[i]
		if sibling.Sum == nil || sibling.Sum.Sign() < 0 {
			return false, ErrNegativeBalanceProhibited
		}

		prevSum := new(big.Int).Set(currSum)
		if proof.IsLeft[i] {
			currSum = new(big.Int).Add(sibling.Sum, prevSum)
			currHash = HashSumNodes(sibling.Hash, sibling.Sum, currHash, prevSum)
		} else {
			currSum = new(big.Int).Add(prevSum, sibling.Sum)
			currHash = HashSumNodes(currHash, prevSum, sibling.Hash, sibling.Sum)
		}
	}

	if currHash != root.Hash {
		return false, fmt.Errorf("%w: root hash mismatch", ErrInvalidProof)
	}
	return true, nil
}
