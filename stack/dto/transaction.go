// Copyright 2018 The trust-net Authors
// Common DTO types used throughout DLT stack
package dto

import (
	"crypto/sha512"
	//	"encoding/binary"
	"github.com/trust-net/dag-lib-go/common"
)

type Transaction interface {
	Id() [64]byte
	Serialize() ([]byte, error)
	DeSerialize(data []byte) error
	Anchor() *Anchor
	Self() *transaction
}

// transaction message
type transaction struct {
	// id of the transaction created from its hash
	id     [64]byte
	idDone bool
	// serialized transaction payload
	Payload []byte
	// transaction payload signature by the submitter
	Signature []byte
	// transaction anchor from DLT stack
	TxAnchor *Anchor
}

// compute SHA512 hash or return from cache
func (tx *transaction) Id() [64]byte {
	if tx.idDone {
		return tx.id
	}
	data := make([]byte, 0)
	// signature should be sufficient to capture payload and submitter ID
	data = append(data, tx.Signature...)
	// append anchor's signature
	data = append(data, tx.TxAnchor.Signature...)
	tx.id = sha512.Sum512(data)
	tx.idDone = true
	return tx.id
}

func (tx *transaction) Serialize() ([]byte, error) {
	return common.Serialize(tx)
}

func (tx *transaction) DeSerialize(data []byte) error {
	if err := common.Deserialize(data, tx); err != nil {
		return err
	}
	return nil
}

func (tx *transaction) Anchor() *Anchor {
	return tx.TxAnchor
}

func (tx *transaction) Self() *transaction {
	return tx
}

// make sure any Transaction can only be created with an anchor
func NewTransaction(a *Anchor) *transaction {
	if a == nil {
		return nil
	}
	return &transaction{
		TxAnchor: a,
	}
}
