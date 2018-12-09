// Copyright 2018 The trust-net Authors
// A network counter application to test DLT Stack library
package main

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha512"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/trust-net/dag-lib-go/db"
	"github.com/trust-net/dag-lib-go/stack"
	"github.com/trust-net/dag-lib-go/stack/dto"
	"github.com/trust-net/dag-lib-go/stack/p2p"
	"github.com/trust-net/go-trust-net/common"
	"math/big"
	"os"
	"strconv"
	"strings"
)

var cmdPrompt = "<headless>: "

var shardId []byte

var key *ecdsa.PrivateKey

var submitter []byte

var myDb = db.NewInMemDatabase("countr_app")

type testTx struct {
	Op     string
	Target string
	Delta  int64
}

func sign(tx *dto.Transaction) *dto.Transaction {
	// sign the test payload using SHA512 hash and ECDSA private key
	type signature struct {
		R *big.Int
		S *big.Int
	}
	s := signature{}
	hash := sha512.Sum512(tx.Payload)
	s.R, s.S, _ = ecdsa.Sign(rand.Reader, key, hash[:])
	tx.Signature, _ = common.Serialize(s)
	tx.Submitter = submitter
	return tx
}

func incrementTx(a *dto.Anchor, name string, delta int) *dto.Transaction {
	if a == nil {
		return nil
	}
	applyDelta(name, delta)
	tx := testTx{
		Op:     "incr",
		Target: name,
		Delta:  int64(delta),
	}
	txPayload, _ := common.Serialize(tx)
	return sign(&dto.Transaction{
		Payload:     txPayload,
		Submitter:   []byte("countr CLI"),
		ShardId:     a.ShardId,
		ShardSeq:    a.ShardSeq,
		ShardParent: a.ShardParent,
	})
}

func decrementTx(a *dto.Anchor, name string, delta int) *dto.Transaction {
	if a == nil {
		return nil
	}
	applyDelta(name, -delta)
	tx := testTx{
		Op:     "decr",
		Target: name,
		Delta:  int64(delta),
	}
	txPayload, _ := common.Serialize(tx)
	return sign(&dto.Transaction{
		Payload:     txPayload,
		Submitter:   []byte("countr CLI"),
		ShardId:     a.ShardId,
		ShardSeq:    a.ShardSeq,
		ShardParent: a.ShardParent,
	})
}

type op struct {
	name  string
	delta int
}

func scanOps(scanner *bufio.Scanner) (ops []op) {
	nextToken := func() (*string, int, bool) {
		if !scanner.Scan() {
			return nil, 0, false
		}
		word := scanner.Text()
		if delta, err := strconv.Atoi(word); err == nil {
			return nil, delta, true
		} else {
			return &word, 0, true
		}
	}
	ops = make([]op, 0)
	currOp := op{}
	readName := false
	for {
		name, delta, success := nextToken()

		if !success {
			if readName {
				ops = append(ops, currOp)
			}
			return
		} else if name == nil && currOp.name == "" {
			return
		}

		if name != nil {
			if readName {
				ops = append(ops, currOp)
			}
			currOp = op{}
			currOp.name = *name
			currOp.delta = 1
			readName = true
		} else {
			currOp.delta = delta
			ops = append(ops, currOp)
			currOp = op{}
			readName = false
		}
	}
}

func applyDelta(name string, delta int) int64 {
	last := int64(0)
	if val, err := myDb.Get([]byte(name)); err == nil {
		common.Deserialize(val, &last)
	}
	last += int64(delta)
	newVal, _ := common.Serialize(last)
	myDb.Put([]byte(name), newVal)
	return last
}

func txHandler(tx *dto.Transaction) error {
	fmt.Printf("\n")
	op := testTx{}
	if err := common.Deserialize(tx.Payload, &op); err != nil {
		fmt.Printf("Invalid TX from %x\n%s", tx.NodeId, cmdPrompt)
		return err
	}
	fmt.Printf("TX: %s %s %d\n", op.Op, op.Target, op.Delta)
	delta := 0
	switch op.Op {
	case "incr":
		delta = int(op.Delta)
	case "decr":
		delta = int(-op.Delta)
	}
	fmt.Printf("%s --> %d\n%s", op.Target, applyDelta(op.Target, delta), cmdPrompt)
	return nil
}

// main CLI loop
func cli(dlt stack.DLT) error {
	if err := dlt.Start(); err != nil {
		return err
	}
	for {
		fmt.Printf(cmdPrompt)
		lineScanner := bufio.NewScanner(os.Stdin)
		for lineScanner.Scan() {
			line := lineScanner.Text()
			if len(line) != 0 {
				wordScanner := bufio.NewScanner(strings.NewReader(line))
				wordScanner.Split(bufio.ScanWords)
				for wordScanner.Scan() {
					cmd := wordScanner.Text()
					switch cmd {
					case "quit":
						fallthrough
					case "q":
						dlt.Stop()
						return nil
					case "countr":
						hasNext := wordScanner.Scan()
						oneDone := false
						for hasNext {
							name := wordScanner.Text()
							if len(name) != 0 {
								if oneDone {
									fmt.Printf("\n")
								} else {
									oneDone = true
								}
								// get current network counter value
								if val, err := myDb.Get([]byte(name)); err == nil {
									var last int64
									common.Deserialize(val, &last)
									fmt.Printf("% 10s: %d", name, last)
								} else {
									fmt.Printf("% 10s: not found", name)
								}
							}
							hasNext = wordScanner.Scan()
						}
						if !oneDone {
							fmt.Printf("usage: countr <countr name> ...\n")
						}
					case "incr":
						ops := scanOps(wordScanner)
						if len(ops) == 0 {
							fmt.Printf("usage: incr <countr name> [<integer>] ...\n")
						} else {
							for _, op := range ops {
								fmt.Printf("adding transaction: incr %s %d\n", op.name, op.delta)
								if err := dlt.Submit(incrementTx(dlt.Anchor(), op.name, op.delta)); err != nil {
									fmt.Printf("Error submitting transaction: %s\n", err)
								}
							}
						}
					case "decr":
						ops := scanOps(wordScanner)
						if len(ops) == 0 {
							fmt.Printf("usage: decr <countr name> [<integer>] ...\n")
						} else {
							for _, op := range ops {
								fmt.Printf("adding transaction: decr %s %d\n", op.name, op.delta)
								if err := dlt.Submit(decrementTx(dlt.Anchor(), op.name, op.delta)); err != nil {
									fmt.Printf("Error submitting transaction: %s\n", err)
								}
							}
						}
					case "info":
						for wordScanner.Scan() {
							continue
						}
						if a := dlt.Anchor(); a == nil {
							fmt.Printf("failed to get any info...\n")
						} else {
							fmt.Printf("ShardId: %s\n", a.ShardId)
							fmt.Printf("Next Seq: %d\n", a.ShardSeq)
							fmt.Printf("Parent: %x\n", a.ShardParent)
							fmt.Printf("NodeId: %x\n", a.NodeId)
						}
					case "join":
						if !wordScanner.Scan() {
							fmt.Printf("usage: join <shard id> [<name>]\n")
							break
						}
						name := wordScanner.Text()
						shardId = []byte(name)
						if wordScanner.Scan() {
							name = wordScanner.Text()
						}
						if err := dlt.Register([]byte(shardId), name, txHandler); err != nil {
							fmt.Printf("Error registering app: %s\n", err)
						} else {
							cmdPrompt = "<" + name + ">: "
						}
					case "leave":
						for wordScanner.Scan() {
							continue
						}
						if err := dlt.Unregister(); err != nil {
							fmt.Printf("Error during un-registering app: %s\n", err)
						}
						myDb.Flush()
						cmdPrompt = "<headless>: "
					default:
						fmt.Printf("Unknown Command: %s", cmd)
						for wordScanner.Scan() {
							fmt.Printf(" %s", wordScanner.Text())
						}
						break
					}
				}
			}
			fmt.Printf("\n%s", cmdPrompt)
		}
	}
	return nil
}

func main() {
	fileName := flag.String("config", "", "config file name")
	flag.Parse()
	if len(*fileName) == 0 {
		fmt.Printf("Missing required parameter \"config\"\n")
		return
	}
	// open the config file
	file, err := os.Open(*fileName)
	if err != nil {
		fmt.Printf("Failed to open config file: %s\n", err)
		return
	}
	data := make([]byte, 2048)
	// read config data from file
	config := p2p.Config{}
	if count, err := file.Read(data); err == nil {
		data = data[:count]
		// parse json data into structure
		if err := json.Unmarshal(data, &config); err != nil {
			fmt.Printf("Failed to parse config data: %s\n", err)
			return
		}
	} else {
		fmt.Printf("Failed to read config file: %s\n", err)
		return
	}

	// create a new ECDSA key for submitter client
	key, _ = crypto.GenerateKey()
	submitter = crypto.FromECDSAPub(&key.PublicKey)

	// instantiate the DLT stack
	if dlt, err := stack.NewDltStack(config, db.NewInMemDbProvider()); err != nil {
		fmt.Printf("Failed to create DLT stack: %s", err)
	} else if err = cli(dlt); err != nil {
		fmt.Printf("Error in CLI: %s", err)
	} else {
		fmt.Printf("Shutdown cleanly")
	}
	fmt.Printf("\n")
}
