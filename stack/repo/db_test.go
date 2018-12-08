package repo

import (
	"github.com/trust-net/dag-lib-go/db"
	"github.com/trust-net/dag-lib-go/stack/dto"
	"testing"
)

func TestXYZ(t *testing.T) {

}

func TestInitiatization(t *testing.T) {
	var repo DltDb
	var err error
	testDb := db.NewInMemDbProvider()
	repo, err = NewDltDb(testDb)
	db := repo.(*dltDb)
	if db == nil || err != nil {
		t.Errorf("Initiatization validation failed, c: %s, err: %s", repo, err)
	}
	if db.txDb != testDb.DB("dlt_transactions") {
		t.Errorf("Incorrect Transaction DB reference expected: %s, actual: %s", testDb.DB("dlt_transactions").Name(), db.txDb.Name())
	}
	if db.shardDb != testDb.DB("dlt_shards") {
		t.Errorf("Incorrect Shards DB reference expected: %s, actual: %s", testDb.DB("dlt_shards").Name(), db.shardDb.Name())
	}
	if db.submitterDb != testDb.DB("dlt_submitters") {
		t.Errorf("Incorrect Submitters DB reference expected: %s, actual: %s", testDb.DB("dlt_submitters").Name(), db.submitterDb.Name())
	}
	if db.dagDb != testDb.DB("dlt_dags") {
		t.Errorf("Incorrect DAGs DB reference expected: %s, actual: %s", testDb.DB("dlt_dags").Name(), db.dagDb.Name())
	}
}

// test adding transaction
func TestAddTx(t *testing.T) {
	repo, _ := NewDltDb(db.NewInMemDbProvider())
	tx := dto.TestSignedTransaction("test data")
	txId := tx.Id()
	tx_ser, _ := tx.Serialize()

	// save transaction
	if err := repo.AddTx(tx); err != nil {
		t.Errorf("Failed to add transaction: %s", err)
	}

	// validate that transaction was added to Transaction DB correctly
	if got_tx, err := repo.txDb.Get(txId[:]); err != nil {
		t.Errorf("Error in checking transaction DB: %s", err)
	} else if string(got_tx) != string(tx_ser) {
		t.Errorf("Got incorrect transaction\nExpected: %x\nActual: %x", tx_ser, got_tx)
	}
	
	// validate that shard DAG node was added for the transaction correctly
	if dagNode, _ := repo.shardDb.Get(txId[:]); dagNode == nil {
		t.Errorf("Did not save DAG node in shard DB")
	}
}

// test shard DAG update during adding transaction
func TestAddTxShardDagUpdate(t *testing.T) {
	repo, _ := NewDltDb(db.NewInMemDbProvider())
	parent := dto.TestSignedTransaction("test data")
	child1 := dto.TestSignedTransaction("test data")
	child1.ShardParent = parent.Id()
	child2 := dto.TestSignedTransaction("test data")
	child2.ShardParent = parent.Id()

	// save transactions
	repo.AddTx(parent)
	repo.AddTx(child1)
	repo.AddTx(child2)
	
	// validate that shard DAG node was added for the transactions correctly
	if parentNode := repo.GetShardDagNode(parent.Id()); parentNode == nil {
		t.Errorf("Did not save DAG node in shard DB")
	} else {
		// validate that children nodes were added correctly for parent's DAG node
		if len(parentNode.Children) != 2 {
			t.Errorf("Incorrect children count: %d", len(parentNode.Children))
		} else {
			if parentNode.Children[0] != child1.Id() {
				t.Errorf("Incorrect 1st child\nExpected: %x\nActual: %x", child1.Id(), parentNode.Children[0])
			}
			if parentNode.Children[1] != child2.Id() {
				t.Errorf("Incorrect 2nd child\nExpected: %x\nActual: %x", child2.Id(), parentNode.Children[1])
			}
		}
	}
}

// test adding duplicate transaction
func TestAddDuplicateTx(t *testing.T) {
	repo, _ := NewDltDb(db.NewInMemDbProvider())
	tx := dto.TestSignedTransaction("test data")

	// save transaction twice
	repo.AddTx(tx)
	if err := repo.AddTx(tx); err == nil {
		t.Errorf("Failed to detect duplicate transaction")
	}
}

// test adding transaction with no parent (DB will add, assumption is that sharding or endorser layer check for orphan)
func TestAddOrphanTx(t *testing.T) {
	repo, _ := NewDltDb(db.NewInMemDbProvider())
	tx := dto.TestSignedTransaction("test data")
	
	// make transaction orphan
	tx.ShardSeq = [8]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02}
	parent := []byte("some random parent")
	for i := 0; i < len(tx.ShardParent) && i < len(parent); i++ {
		tx.ShardParent[i] = parent[i]
	}

	// save the orphaned transaction
	if err := repo.AddTx(tx); err != nil {
		t.Errorf("Failed to add orphan transaction: %s", err)
	}
}

// test getting a transaction after adding
func TestGetTx(t *testing.T) {
	repo, _ := NewDltDb(db.NewInMemDbProvider())

	// first try to get a transaction without adding
	tx := dto.TestSignedTransaction("test data")
	if repo.GetTx(tx.Id()) != nil {
		t.Errorf("Did not expect a transaction that is not saved yet!!!")
	}

	// now save transaction and then get, this time it should work
	repo.AddTx(tx)
	got_tx := repo.GetTx(tx.Id())
	if got_tx == nil {
		t.Errorf("Did not get a saved transaction!!!")
	} else if got_tx.Id() != tx.Id() {
		t.Errorf("Got incorrect transaction\nExpected: %x\nActual: %x", tx.Id(), got_tx.Id())
	}
}

// test deleting a transaction
func TestDeleteTx(t *testing.T) {
	repo, _ := NewDltDb(db.NewInMemDbProvider())

	// first save a transaction
	tx := dto.TestSignedTransaction("test data")
	txId := tx.Id()
	repo.AddTx(tx)

	// now delete transaction
	if err := repo.DeleteTx(tx.Id()); err != nil {
		t.Errorf("Failed to delete transaction: %s", err)
	}

	// validate that transaction was deleted from Transaction DB correctly
	if got_tx, _ := repo.txDb.Get(txId[:]); got_tx != nil {
		t.Errorf("Transaction not deleted from DB")
	}
}

// test get shard DAG after adding transaction
func TestGetShardDagNode(t *testing.T) {
	repo, _ := NewDltDb(db.NewInMemDbProvider())
	tx := dto.TestSignedTransaction("test data")

	// save transaction
	repo.AddTx(tx)

	// validate that can get shard DAG node after adding transaction
	if dagNode := repo.GetShardDagNode(tx.Id()); dagNode == nil {
		t.Errorf("Cannot get DAG node in shard DB")
	}
}
