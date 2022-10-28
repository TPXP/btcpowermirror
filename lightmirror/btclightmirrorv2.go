// Copyright (c) 2021 The powermirror developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package lightmirror

import (
	"errors"
	"fmt"
	"io"
	
	"github.com/btcsuite/btcd/blockchain"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/ethereum/go-ethereum/common"
)

const (
	powerMagicString = "CORE"
)

// BtcLightMirrorV2 defines information about a block and is used in the bitcoin
// block (BtcBlock) and headers (MsgHeaders) messages.
type BtcLightMirrorV2 struct {
	BtcHeader wire.BlockHeader

	CoinBaseTx wire.MsgTx

	TransactionSize int

	MerkleNodes []chainhash.Hash
}

func CreateBtcLightMirrorV2(btcHeader *wire.BlockHeader, coinBaseTx *wire.MsgTx, transactions []chainhash.Hash) *BtcLightMirrorV2 {

	merkles := BuildMerkleTreeStore(&transactions[0], transactions[1:])

	txSize := len(transactions)
	exponent := getExponent(txSize)
	merkleNodes := make([]chainhash.Hash, 0, exponent)
	offset := 1 << exponent
	lastIndex := 1
	for i := 0; i < exponent; i++ {
		merkleNodes = append(merkleNodes, *merkles[lastIndex])
		lastIndex += offset
		offset >>= 1
	}

	return &BtcLightMirrorV2{
		*btcHeader,
		*coinBaseTx,
		txSize,
		merkleNodes,
	}
}

// Deserialize decodes a block header from r into the receiver using a format.
func (light *BtcLightMirrorV2) Deserialize(r io.Reader) error {
	err := light.BtcHeader.Deserialize(r)
	if err != nil {
		return err
	}

	err = light.CoinBaseTx.Deserialize(r)
	if err != nil {
		return err
	}

	txSize, err := wire.ReadVarInt(r, 0)
	if err != nil {
		return err
	}

	// Prevent more transactions than could possibly fit into a block.
	// It would be possible to cause memory exhaustion and panics without
	// a sane upper bound on this count.
	if txSize > maxTxPerBlock {
		return fmt.Errorf("BtcBlock.BtcDecode too many transactions to fit "+
			"into a block [count %d, max %d]", txSize, maxTxPerBlock)
	}

	merkleNodeSize := getExponent(int(txSize))
	light.TransactionSize = int(txSize)
	light.MerkleNodes = make([]chainhash.Hash, merkleNodeSize, merkleNodeSize)
	for i := 0; i < merkleNodeSize; i++ {
		_, err := io.ReadFull(r, light.MerkleNodes[i][:])
		if err != nil {
			return err
		}
	}

	return nil
}

// Serialize encodes a block header to w from the receiver using a format.
func (light *BtcLightMirrorV2) Serialize(w io.Writer) error {
	err := light.BtcHeader.Serialize(w)
	if err != nil {
		return err
	}

	err = light.CoinBaseTx.Serialize(w)
	if err != nil {
		return err
	}

	err = wire.WriteVarInt(w, 0, uint64(light.TransactionSize))
	if err != nil {
		return err
	}

	for _, txHash := range light.MerkleNodes {
		_, err := w.Write(txHash[:])
		if err != nil {
			return err
		}
	}

	return nil
}

func (light *BtcLightMirrorV2) ParsePowerParams() (candidateAddr common.Address, rewardAddr common.Address, blockHash chainhash.Hash) {
	for _, txout := range light.CoinBaseTx.TxOut[1:] {
		pkScript := txout.PkScript
		if len(pkScript) < 1+1+4+1+20+20 || pkScript[0] != txscript.OP_RETURN || string(pkScript[2:6]) != powerMagicString || pkScript[6] != txscript.OP_DATA_1 {
			continue
		}
		candidateAddr = common.BytesToAddress(pkScript[7:27])
		rewardAddr = common.BytesToAddress(pkScript[27:47])
		if len(pkScript) >= 47+32 {
			bh, _ := chainhash.NewHash(pkScript[47 : 47+32])
			blockHash = *bh
		}
	}
	return
}

func (light *BtcLightMirrorV2) CheckMerkle() error {
	coinbaseHash := light.CoinBaseTx.TxHash()
	root := calculateMerkleRoot(&coinbaseHash, light.MerkleNodes)
	if !light.BtcHeader.MerkleRoot.IsEqual(&root) {
		str := fmt.Sprintf("block merkle root is invalid - block "+
			"header indicates %v, but calculated value is %v",
			light.BtcHeader.MerkleRoot, root)
		return errors.New(str)
	}
	return nil
}

func calculateMerkleRoot(coinbaseHash *chainhash.Hash, merkleNodes []chainhash.Hash) chainhash.Hash {
	res := coinbaseHash
	for _, node := range merkleNodes {
		res = blockchain.HashMerkleBranches(res, &node)
	}
	return *res
}

func getExponent(v int) int {
	res := 0
	for ; v > (1 << res); res++ {
	}
	return res
}