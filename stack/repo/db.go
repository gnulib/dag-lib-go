// Copyright 2018-2019 The trust-net Authors
// A database repo to handle DLT state and history
package repo

import (
	"errors"
	"github.com/trust-net/dag-lib-go/common"
	"github.com/trust-net/dag-lib-go/db"
	"github.com/trust-net/dag-lib-go/stack/dto"
//	"sync"
)

type DagNode struct {
	// parent node in the DAG
	Parent [64]byte
	// children nodes in the DAG
	Children [][64]byte
	// reference to Transaction ID in transaction table
	TxId [64]byte
	// Depth of the node in DAG
	Depth uint64
}

type ShardTxPair struct {
	// Shard ID
	ShardId []byte
	// Tx ID
	TxId [64]byte
}
type SubmitterHistory struct {
	// Submitter ID
	Submitter []byte
	// Submitter Seq
	Seq uint64
	// shard id: tx id list
	ShardTxPairs []ShardTxPair
}

type DltDb interface {
	// get a transaction from transaction history (no entry == nil)
	GetTx(id [64]byte) dto.Transaction
	// add a new transaction to transaction history (no duplicates, no updates)
	AddTx(tx dto.Transaction) error
	// update a shard's DAG and tips for a new transaction
	UpdateShard(tx dto.Transaction) error
	// flush a shard DAG
	FlushShard(shardId []byte) error
	// update a submitter's DAG and tips for a new transaction
	UpdateSubmitter(tx dto.Transaction) error
	// replace a submitter's DAG and tips for a new transaction
	ReplaceSubmitter(tx dto.Transaction) error
	// delete an existing transaction from transaction history (deleting a non-tip transaction will cause errors)
	DeleteTx(id [64]byte) error
	// get the shard's DAG node for given transaction Id (no entry == nil)
	GetShardDagNode(id [64]byte) *DagNode
	// get the submitter's history for specified submitter id and seq
	GetSubmitterHistory(id []byte, seq uint64) *SubmitterHistory
	// get list of shards seen so far based on transaction history
	GetShards() []byte
	// get list of submitters seen so far based on transaction history
	GetSubmitters() []byte
	// get tip DAG nodes for sharder's DAG
	ShardTips(shardId []byte) [][64]byte
	// get tip DAG nodes for submmiter's DAG
	SubmitterTips(submitterId []byte) []DagNode
}

type dltDb struct {
	txDb               db.Database
	shardDAGsDb        db.Database
	shardTipsDb        db.Database
	submitterHistoryDb db.Database
//	lock               sync.RWMutex
}

func (d *dltDb) GetTx(id [64]byte) dto.Transaction {
//	d.lock.Lock()
//	defer d.lock.Unlock()
	// get serialized transactions from DB
	if data, err := d.txDb.Get(id[:]); err != nil {
		return nil
	} else {
		// deserialize the transaction read from DB
		tx := dto.NewTransaction(&dto.TxRequest{}, &dto.Anchor{})
		if err := tx.DeSerialize(data); err != nil {
			return nil
		}
		return tx
	}
}
func (d *dltDb) AddTx(tx dto.Transaction) error {
	// save transaction
	var data []byte
	var err error
	if data, err = tx.Serialize(); err != nil {
		return err
	}
//	d.lock.Lock()
//	defer d.lock.Unlock()
	// check for duplicate transaction
	id := tx.Id()
	if present, _ := d.txDb.Has(id[:]); present {
		return errors.New("duplicate transaction")
	}

	// save the transaction in DB
	if err = d.txDb.Put(id[:], data); err != nil {
		return err
	}
	return nil
}

func (d *dltDb) FlushShard(shardId []byte) error {
//	d.lock.Lock()
//	defer d.lock.Unlock()
	// walk through shard's tips, traverse up and remove
	tipNodes := []*DagNode{}
	for _, tip := range d.shardTips(shardId) {
		tipNodes = append(tipNodes, d.getShardDagNode(tip))
	}
	if err := d.shardTipsDb.Delete(shardId); err != nil {
		return err
	}
	for len(tipNodes) > 0 {
		// pop a dag node
		node := tipNodes[0]
		tipNodes = tipNodes[1:]
		if parent := d.getShardDagNode(node.Parent); parent != nil {
			tipNodes = append(tipNodes, parent)
		}
		// remove current node
		if err := d.shardDAGsDb.Delete(node.TxId[:]); err != nil {
			return err
		}
	}
	return nil
}

func (d *dltDb) UpdateShard(tx dto.Transaction) error {
	// save transaction
	var err error
//	d.lock.Lock()
//	defer d.lock.Unlock()

	// add the DAG node for the transaction to shard DAG db
	dagNode := DagNode{
		Parent: tx.Anchor().ShardParent,
		TxId:   tx.Id(),
		Depth:  tx.Anchor().ShardSeq,
	}
	if err = d.saveShardDagNode(&dagNode); err != nil {
		return err
	}

	// update the children of the parent DAG (if present)
	if parent := d.getShardDagNode(tx.Anchor().ShardParent); parent != nil {
		parent.Children = append(parent.Children, tx.Id())
		if err := d.saveShardDagNode(parent); err != nil {
			return err
		}
	}

	// remove parent and uncles from shard's TIPs (if present)
	tips := d.shardTips(tx.Request().ShardId)
	newTips := make([][64]byte, 0, len(tips))
	uncles := make(map[[64]byte]struct{})
	for _, uncle := range tx.Anchor().ShardUncles {
		uncles[uncle] = struct{}{}
	}
	for _, tip := range tips {
		if _, isUncle := uncles[tip]; tip != tx.Anchor().ShardParent && !isUncle {
			newTips = append(newTips, tip)
		} else {
			// fmt.Printf("removing parent tip: %x\n", tip)
		}
	}
	// add new transaction to the shard's tips
	newTips = append(newTips, tx.Id())
	// fmt.Printf("adding child tip: %x\n", tx.Id())
	// update shard's tips
	if err = d.updateShardTips(tx.Request().ShardId, newTips); err != nil {
		return err
	}

	return nil
}

func (d *dltDb) saveShardDagNode(node *DagNode) error {
	var data []byte
	var err error
	if data, err = common.Serialize(node); err != nil {
		return err
	}
	if err = d.shardDAGsDb.Put(node.TxId[:], data); err != nil {
		return err
	}
	return nil
}

func (d *dltDb) ReplaceSubmitter(tx dto.Transaction) error {
//	d.lock.Lock()
//	defer d.lock.Unlock()

	// lookup submitter history, if present
	var history *SubmitterHistory
	if history = d.getSubmitterHistory(tx.Request().SubmitterId, tx.Request().SubmitterSeq); history == nil {
		history = &SubmitterHistory{
			Submitter:    tx.Request().SubmitterId,
			Seq:          tx.Request().SubmitterSeq,
			ShardTxPairs: make([]ShardTxPair, 0, 1),
		}
	}

	// remove any pre-existing shard/tx in history
	found := false
	newPair := ShardTxPair{
		ShardId: tx.Request().ShardId,
		TxId:    tx.Id(),
	}
	for i, existingPair := range history.ShardTxPairs {
		if string(existingPair.ShardId) == string(newPair.ShardId) {
			// there is some tx for same shard, replace this new pair
			history.ShardTxPairs[i] = newPair
			found = true
		}
	}

	// add the new shard/tx pair to history if not replaced old
	if !found {
		history.ShardTxPairs = append(history.ShardTxPairs, newPair)
	}
	// update the submitter history
	if data, err := common.Serialize(history); err != nil {
		return err
	} else if err := d.submitterHistoryDb.Put(submitterHistoryKey(history.Submitter, history.Seq), data); err != nil {
		return err
	}

	return nil
}

func (d *dltDb) UpdateSubmitter(tx dto.Transaction) error {
//	d.lock.Lock()
//	defer d.lock.Unlock()

	// lookup submitter history, if present
	var history *SubmitterHistory
	if history = d.getSubmitterHistory(tx.Request().SubmitterId, tx.Request().SubmitterSeq); history == nil {
		history = &SubmitterHistory{
			Submitter:    tx.Request().SubmitterId,
			Seq:          tx.Request().SubmitterSeq,
			ShardTxPairs: make([]ShardTxPair, 0, 1),
		}
	}

	// make sure there is no pre-existing shard/tx in history
	newPair := ShardTxPair{
		ShardId: tx.Request().ShardId,
		TxId:    tx.Id(),
	}
	for _, existingPair := range history.ShardTxPairs {
		if string(existingPair.ShardId) == string(newPair.ShardId) {
			// there is some tx for same shard, if its same tx then silently return
			if existingPair.TxId == newPair.TxId {
				return nil
			} else {
				// double spending error
				return errors.New("double spending tx")
			}
		}
	}

	// add the new shard/tx pair to history
	history.ShardTxPairs = append(history.ShardTxPairs, newPair)

	// update the submitter history
	if data, err := common.Serialize(history); err != nil {
		return err
	} else if err := d.submitterHistoryDb.Put(submitterHistoryKey(history.Submitter, history.Seq), data); err != nil {
		return err
	}

	return nil
}

func (d *dltDb) DeleteTx(id [64]byte) error {
//	d.lock.Lock()
//	defer d.lock.Unlock()
	// TBD: check that its a tip transaction, otherwise cannot delete

	if err := d.txDb.Delete(id[:]); err != nil {
		return err
	}

	// TBD: remove from DAGs and update tips
	return nil
}

func (d *dltDb) GetShardDagNode(id [64]byte) *DagNode {
//	d.lock.Lock()
//	defer d.lock.Unlock()
	return d.getShardDagNode(id)
}

func (d *dltDb) getShardDagNode(id [64]byte) *DagNode {
	// get serialized DAG node from DB
	if data, err := d.shardDAGsDb.Get(id[:]); err != nil {
		return nil
	} else {
		// deserialize the DAG node read from DB
		dagNode := &DagNode{}
		if err := common.Deserialize(data, dagNode); err != nil {
			return nil
		}
		return dagNode
	}
}

func submitterHistoryKey(id []byte, seq uint64) []byte {
	// build submitter history key as submitter ID + ":" + submitter seq
	key := []byte{}
	key = append(key, id...)
	key = append(key, ':')
	key = append(key, common.Uint64ToBytes(seq)...)
	return key
}

func (d *dltDb) GetSubmitterHistory(id []byte, seq uint64) *SubmitterHistory {
//	d.lock.Lock()
//	defer d.lock.Unlock()

	// get the submitter history
	return d.getSubmitterHistory(id, seq)
}

func (d *dltDb) getSubmitterHistory(id []byte, seq uint64) *SubmitterHistory {
	// get the submitter history
	if data, err := d.submitterHistoryDb.Get(submitterHistoryKey(id, seq)); err != nil {
		return nil
	} else {
		history := &SubmitterHistory{}
		if err := common.Deserialize(data, history); err != nil {
			return nil
		}
		return history
	}
}

func (d *dltDb) GetShards() []byte {
	return nil
}

func (d *dltDb) GetSubmitters() []byte {
	return nil
}

func (d *dltDb) ShardTips(shardId []byte) [][64]byte {
//	d.lock.Lock()
//	defer d.lock.Unlock()
	return d.shardTips(shardId)
}

func (d *dltDb) shardTips(shardId []byte) [][64]byte {
	// get serialized tips from DB
	if data, err := d.shardTipsDb.Get(shardId); err != nil {
		return nil
	} else {
		// deserialize the tips read from DB
		tips := [][64]byte{}
		if err := common.Deserialize(data, &tips); err != nil {
			return nil
		}
		return tips
	}
	return nil
}

func (d *dltDb) updateShardTips(shardId []byte, tips [][64]byte) error {
	var data []byte
	var err error
	if data, err = common.Serialize(tips); err != nil {
		return err
	}
	if err = d.shardTipsDb.Put(shardId, data); err != nil {
		return err
	}

	return nil
}

func (d *dltDb) SubmitterTips(submitterId []byte) []DagNode {
	return nil
}

func NewDltDb(dbp db.DbProvider) (*dltDb, error) {
	return &dltDb{
		txDb:               dbp.DB("dlt_transactions"),
		shardDAGsDb:        dbp.DB("dlt_shard_dags"),
		shardTipsDb:        dbp.DB("dlt_shard_tips"),
		submitterHistoryDb: dbp.DB("dlt_submitter_history"),
	}, nil
}
