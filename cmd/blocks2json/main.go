package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"github.com/kaspanet/kaspad/app/appmessage"
	"github.com/kaspanet/kaspad/domain/consensus"
	"github.com/kaspanet/kaspad/domain/consensus/model"
	"github.com/kaspanet/kaspad/domain/consensus/utils/hashset"
	"github.com/kaspanet/kaspad/domain/dagconfig"
	"os"
)

func main() {
	//cfg, err := parseConfig()
	//if err != nil {
	//	panic(err)
	//}
	reverse()
	os.Exit(0)

	consensusConfig := &consensus.Config{Params: dagconfig.DevnetParams}
	consensusConfig.SkipProofOfWork = true
	dbPath := "/tmp/local_devnet1/kaspa-devnet/datadir2/"
	factory := consensus.NewFactory()
	factory.SetTestDataDir(dbPath)
	tc, tearDownFunc, err := factory.NewTestConsensus(consensusConfig, "blocks2json")
	if err != nil {
		panic(err)
	}
	defer tearDownFunc(true)

	f, err := os.OpenFile("/tmp/out.json.gz", os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	gzipWriter := gzip.NewWriter(f)
	defer gzipWriter.Close()

	w := json.NewEncoder(gzipWriter)
	err = w.Encode(consensusConfig.Params)
	if err != nil {
		panic(err)
	}

	visited := hashset.New()
	queue := tc.DAGTraversalManager().NewUpHeap(model.NewStagingArea())
	err = queue.Push(consensusConfig.GenesisHash)
	if err != nil {
		panic(err)
	}

	i := 0
	txs := 0
	pps := hashset.New()
	for queue.Len() > 0 {
		current := queue.Pop()
		if visited.Contains(current) || current.Equal(model.VirtualBlockHash) {
			continue
		}
		visited.Add(current)

		i++
		fmt.Printf("Processed %d blocks and %d transactions: %s\n", i, txs, current)

		block, err := tc.BlockStore().Block(tc.DatabaseContext(), model.NewStagingArea(), current)
		if err != nil {
			panic(err)
		}
		txs += len(block.Transactions)
		pps.Add(block.Header.PruningPoint())

		rpcBlock := appmessage.DomainBlockToRPCBlock(block)
		rpcBlock.VerboseData = &appmessage.RPCBlockVerboseData{Hash: current.String()}
		err = w.Encode(rpcBlock)
		if err != nil {
			panic(err)
		}

		relation, err := tc.BlockRelationStore().BlockRelation(tc.DatabaseContext(), model.NewStagingArea(), current)
		if err != nil {
			panic(err)
		}

		err = queue.PushSlice(relation.Children)
		if err != nil {
			panic(err)
		}
	}

	fmt.Printf("PPS: %+v\n", pps)
}

func reverse() {
	consensusConfig := &consensus.Config{Params: dagconfig.DevnetParams}
	consensusConfig.SkipProofOfWork = true
	factory := consensus.NewFactory()
	tc, tearDownFunc, err := factory.NewTestConsensus(consensusConfig, "blocks2json")
	if err != nil {
		panic(err)
	}
	defer tearDownFunc(true)

	f, err := os.OpenFile("/home/ori/rusty-kaspa/consensus/tests/testdata/json_test.json.gz", os.O_RDONLY, 0600)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	gzipReader, err := gzip.NewReader(f)
	if err != nil {
		panic(err)
	}
	defer gzipReader.Close()

	r := json.NewDecoder(gzipReader)
	//err = r.Encode(consensusConfig.Params)
	//if err != nil {
	//	panic(err)
	//}

	for i := 0; ; i++ {
		rpcBlock := &appmessage.RPCBlock{}
		err := r.Decode(rpcBlock)
		if err != nil {
			panic(err)
		}

		if i == 0 {
			continue
		}

		block, err := appmessage.RPCBlockToDomainBlock(rpcBlock)
		if err != nil {
			panic(err)
		}

		err = tc.ValidateAndInsertBlock(block, true)
		if err != nil {
			panic(err)
		}

		if i%100 == 0 {
			fmt.Printf("Validated %d blocks\n", i+1)
		}
	}
}
