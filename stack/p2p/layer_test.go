// Copyright 2018-2019 The trust-net Authors
package p2p

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/trust-net/dag-lib-go/stack/dto"
	"math/big"
	"testing"
)

func TestDEVp2pInstance(t *testing.T) {
	var p2p Layer
	var err error
	// test and validate p2pImpl is a P2P
	conf := TestConfig()
	p2p, err = NewDEVp2pLayer(conf, func(peer Peer) error { return nil })
	if err != nil {
		t.Errorf("Failed to get P2P layer instance: %s", err)
	}
	if impl, ok := p2p.(*layerDEVp2p); ok {
		fmt.Printf("p2p layer type: %T\n", impl)
	} else {
		t.Errorf("Failed to cast P2P layer into DEVp2p implementation")
	}
	// p2p node's ID should be initialized correctly
	if string(p2p.Id()) != string(crypto.FromECDSAPub(&p2p.(*layerDEVp2p).conf.PrivateKey.PublicKey)) {
		t.Errorf("Did not initialize p2p node's ID")
	}
	// peers map should be initialized correctly
	if p2p.(*layerDEVp2p).peers == nil || len(p2p.(*layerDEVp2p).peers) != 0 {
		t.Errorf("Did not initialize P2P Layer's peers map")
	}
}

func TestDEVp2pInstanceBadConfig(t *testing.T) {
	_, err := NewDEVp2pLayer(Config{}, func(peer Peer) error { return nil })
	if err == nil {
		t.Errorf("Expected no instance due to bad config")
	}
}

func TestDEVp2pRunner(t *testing.T) {
	// flags to check from inside callback
	called := false
	peerInMap := false
	// create an instance of DEVp2p layer
	var layer *layerDEVp2p
	layer, _ = NewDEVp2pLayer(TestConfig(), func(peer Peer) error {
		called = true
		_, peerInMap = layer.peers[string(peer.ID())]
		return nil
	})
	// invoke runner with a mock p2p peer node and connection
	mPeer := TestDEVp2pPeer("mock peer")
	mConn := TestConn()
	layer.runner(mPeer, mConn)
	if !called {
		t.Errorf("Callback did not get called")
	}
	// validate that peer got added to map before callback
	if !peerInMap {
		t.Errorf("peer did not get added to map before callback")
	}
	// validate that peer got removed from map after callback
	if _, peerInMap = layer.peers[string(mPeer.ID().Bytes())]; peerInMap {
		t.Errorf("peer did not get removed from map after callback")
	}
}

func TestDEVp2pSign(t *testing.T) {
	// create an instance of the p2p layer
	conf := TestConfig()
	p2p, _ := NewDEVp2pLayer(conf, func(peer Peer) error { return nil })

	// create a test payload
	payload := []byte("test data")

	// get key from test config
	key, _ := conf.key()

	// sign the test payload
	if sign, err := p2p.Sign(payload); err != nil {
		t.Errorf("Failed to get p2p signature: %s", err)
	} else {
		if len(sign) != 64 {
			t.Errorf("Incorrect signature length: %d", len(sign))
			return
		}
		// regenerate signature parameters
		s := signature{
			R: &big.Int{},
			S: &big.Int{},
		}
		s.R.SetBytes(sign[0:32])
		s.S.SetBytes(sign[32:64])
		// we want to validate the SHA256 digest of the payload
		hash := sha256.Sum256(payload)
		// validate signature of payload
		if !ecdsa.Verify(&key.PublicKey, hash[:], s.R, s.S) {
			t.Errorf("signature validation failed")
		}
	}
}

func TestDEVp2pVerify(t *testing.T) {
	// create an instance of the p2p layer
	p2p, _ := NewDEVp2pLayer(TestConfig(), func(peer Peer) error { return nil })

	// create a test payload
	payload := []byte("test data")

	// create a new ECDSA key
	key, _ := crypto.GenerateKey()
	id := crypto.FromECDSAPub(&key.PublicKey)

	// sign the test payload using SHA256 digest and ECDSA private key
	s := signature{}
	hash := sha256.Sum256(payload)
	s.R, s.S, _ = ecdsa.Sign(rand.Reader, key, hash[:])
	sign := append(s.R.Bytes(), s.S.Bytes()...)

	// validate that p2p layer can verify the signature
	if !p2p.Verify(payload, sign, id) {
		t.Errorf("Failed to verify signature")
	}
}

func TestDEVp2pBroadcast(t *testing.T) {
	// create an instance of the p2p layer
	var p2p *layerDEVp2p
	var broadCastError error
	p2p, _ = NewDEVp2pLayer(TestConfig(), func(peer Peer) error {
		// broadcast a message to all peers
		broadCastError = p2p.Broadcast([]byte("test message"), 1, struct{}{})
		return nil
	})
	// invoke runner with a mock p2p peer node and connection
	mPeer := TestDEVp2pPeer("mock peer")
	mConn := TestConn()
	p2p.runner(mPeer, mConn)
	if broadCastError != nil {
		t.Errorf("Failed to broadcast message: %s", broadCastError)
	}
	// we should have sent message on our mock peer connection
	if mConn.WriteCount != 1 {
		t.Errorf("did not write message to peer connection")
	}
}

func TestAnchor(t *testing.T) {
	// create an instance of the p2p layer
	conf := TestConfig()
	p2p, _ := NewDEVp2pLayer(conf, func(peer Peer) error { return nil })

	// build an anchor filled from controller and sharder
	parent := dto.RandomHash()
	uncles := [][64]byte{dto.RandomHash(), dto.RandomHash()}
	a := &dto.Anchor{
		ShardSeq:    0x21,
		Weight:      0xf1,
		ShardParent: parent,
		ShardUncles: uncles,
	}

	// send anchor for processing
	if err := p2p.Anchor(a); err != nil {
		t.Errorf("Anchor handling failed: %s", err)
	}

	// validate that Anchor was updated with node ID correctly
	if string(a.NodeId) != string(p2p.Id()) {
		t.Errorf("Incorrect Anchor ID:\n%x\nExpected:\n%x", a.NodeId, p2p.Id())
	}

	// validate that anchor was signed appropriately
	payload := a.Bytes()
	// regenerate signature parameters
	s := signature{
		R: &big.Int{},
		S: &big.Int{},
	}
	s.R.SetBytes(a.Signature[0:32])
	s.S.SetBytes(a.Signature[32:64])
	// we want to validate the SHA256 digest of the payload
	hash := sha256.Sum256(payload)
	// validate signature of payload
	if !ecdsa.Verify(&p2p.key.PublicKey, hash[:], s.R, s.S) {
		t.Errorf("signature validation failed")
	}
}
