package main

import (
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/kaspanet/kaspad/app/appmessage"
	"github.com/kaspanet/kaspad/domain/consensus"
	"github.com/kaspanet/kaspad/domain/consensus/model"
	"github.com/kaspanet/kaspad/domain/consensus/model/externalapi"
	"github.com/kaspanet/kaspad/domain/consensus/model/testapi"
	"github.com/kaspanet/kaspad/domain/consensus/utils/consensushashing"
	"github.com/kaspanet/kaspad/domain/consensus/utils/hashset"
	"github.com/kaspanet/kaspad/domain/dagconfig"
	"os"
	"path"
)

var mainPath = "/tmp/out"

func main() {
	//cfg, err := parseConfig()
	//if err != nil {
	//	panic(err)
	//}
	//reverse()
	//os.Exit(0)

	consensusConfig := &consensus.Config{Params: dagconfig.MainnetParams}
	//consensusConfig.SkipProofOfWork = true
	dbPath := "/home/ori/.kaspad/kaspa-mainnet/datadir2/"
	factory := consensus.NewFactory()
	factory.SetTestDataDir(dbPath)
	tc, tearDownFunc, err := factory.NewTestConsensus(consensusConfig, "blocks2json")
	if err != nil {
		panic(err)
	}
	defer tearDownFunc(true)

	err = os.MkdirAll(mainPath, 0700)
	if err != nil {
		panic(err)
	}

	err = writeTrustedData(tc)
	if err != nil {
		panic(err)
	}

	err = writePPProof(tc)
	if err != nil {
		panic(err)
	}

	err = writePastPruningPoints(tc)
	if err != nil {
		panic(err)
	}

	err = writePPUTXO(tc)
	if err != nil {
		panic(err)
	}
	//os.Exit(0)

	fileBlocks, err := os.OpenFile(path.Join(mainPath, "blocks.json.gz"), os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		panic(err)
	}
	defer fileBlocks.Close()

	gzipWriterBlocks := gzip.NewWriter(fileBlocks)
	defer gzipWriterBlocks.Close()

	w := json.NewEncoder(gzipWriterBlocks)
	err = w.Encode(consensusConfig.Params)
	if err != nil {
		panic(err)
	}

	pruningPoint, err := tc.PruningPoint()
	if err != nil {
		panic(err)
	}

	visited := hashset.New()
	queue := tc.DAGTraversalManager().NewUpHeap(model.NewStagingArea())
	err = queue.Push(pruningPoint)
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

		rpcBlock := domainBlockToRPCBlockWithHash(block)
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

func writePPProof(tc testapi.TestConsensus) error {
	f, err := os.OpenFile(path.Join(mainPath, "proof.json.gz"), os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	gzipWriter := gzip.NewWriter(f)
	defer gzipWriter.Close()

	w := json.NewEncoder(gzipWriter)
	proof, err := tc.BuildPruningPointProof()
	if err != nil {
		return err
	}

	for _, headers := range proof.Headers {
		rpcBlocks := make([]*appmessage.RPCBlockHeader, len(headers))
		for i, header := range headers {
			rpcBlocks[i] = domainBlockToRPCBlockWithHash(&externalapi.DomainBlock{
				Header: header,
			}).Header
		}

		err := w.Encode(rpcBlocks)
		if err != nil {
			return err
		}
	}

	return nil
}

func writePastPruningPoints(tc testapi.TestConsensus) error {
	f, err := os.OpenFile(path.Join(mainPath, "past-pps.json.gz"), os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	gzipWriter := gzip.NewWriter(f)
	defer gzipWriter.Close()

	w := json.NewEncoder(gzipWriter)
	pruningPointHeaders, err := tc.PruningPointHeaders()
	if err != nil {
		return err
	}

	for _, header := range pruningPointHeaders {
		rpcBlock := domainBlockToRPCBlockWithHash(&externalapi.DomainBlock{
			Header: header,
		})

		err := w.Encode(rpcBlock)
		if err != nil {
			return err
		}
	}

	return nil
}

type jsonBluesAnticoneSizes struct {
	BlueHash     string
	AnticoneSize externalapi.KType
}

type jsonGHOSTDAGData struct {
	BlueScore          uint64
	BlueWork           string
	SelectedParent     string
	MergeSetBlues      []string
	MergeSetReds       []string
	BluesAnticoneSizes []*jsonBluesAnticoneSizes
}

type jsonBlockWithTrustedData struct {
	Block    *appmessage.RPCBlock
	GHOSTDAG *jsonGHOSTDAGData
}

func writeTrustedData(tc testapi.TestConsensus) error {
	f, err := os.OpenFile(path.Join(mainPath, "trusted.json.gz"), os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	gzipWriter := gzip.NewWriter(f)
	defer gzipWriter.Close()

	w := json.NewEncoder(gzipWriter)
	pruningPointAndItsAnticone, err := tc.PruningPointAndItsAnticone()
	if err != nil {
		return err
	}

	//
	//children, err := tc.DAGTopologyManager().Children(model.NewStagingArea(), model.VirtualGenesisBlockHash)
	//if err != nil {
	//	return err
	//}

	queue := tc.DAGTraversalManager().NewUpHeap(model.NewStagingArea())
	added := hashset.New()

	trustedHeaders := make(map[externalapi.DomainHash]*externalapi.TrustedDataDataDAAHeader)

	for _, blockHash := range pruningPointAndItsAnticone {
		//err := queue.Push(blockHash)
		//if err != nil {
		//	return err
		//}
		//added.Add(blockHash)

		//block, err := tc.GetBlock(blockHash)
		//if err != nil {
		//	return err
		//}

		blockDAAWindowHashes, err := tc.BlockDAAWindowHashes(blockHash)
		if err != nil {
			return err
		}

		for i, daaWindowHash := range blockDAAWindowHashes {
			if added.Contains(daaWindowHash) {
				continue
			}

			added.Add(daaWindowHash)

			trustedDataDataDAAHeader, err := tc.TrustedDataDataDAAHeader(blockHash, daaWindowHash, uint64(i))
			if err != nil {
				return err
			}

			trustedHeaders[*daaWindowHash] = trustedDataDataDAAHeader
			err = queue.PushWithGHOSTDAGData(daaWindowHash, trustedDataDataDAAHeader.GHOSTDAGData)
			if err != nil {
				return err
			}
		}
		//
		//ghostdagDataBlockHashes, err := tc.TrustedBlockAssociatedGHOSTDAGDataBlockHashes(blockHash)
		//if err != nil {
		//	return err
		//}
		//
		//for _, ghostdagDataBlockHash := range ghostdagDataBlockHashes {
		//	data, err := tc.TrustedGHOSTDAGData(ghostdagDataBlockHash)
		//	if err != nil {
		//		return err
		//	}
		//	blockWithTrustedData.GHOSTDAGData = append(blockWithTrustedData.GHOSTDAGData, &externalapi.BlockGHOSTDAGDataHashPair{
		//		Hash:         ghostdagDataBlockHash,
		//		GHOSTDAGData: data,
		//	})
		//}

		//err = syncee.ValidateAndInsertBlockWithTrustedData(blockWithTrustedData, false)
		//if err != nil {
		//	return err
		//}
	}

	for queue.Len() != 0 {
		current := queue.Pop()
		if _, ok := trustedHeaders[*current]; !ok {
			panic(fmt.Sprintf("wut %s", current))
		}
		err := w.Encode(jsonBlockWithTrustedData{
			Block: domainBlockToRPCBlockWithHash(&externalapi.DomainBlock{
				Header: trustedHeaders[*current].Header,
			}),
			GHOSTDAG: ghostdagToJSON(trustedHeaders[*current].GHOSTDAGData),
		})
		if err != nil {
			return err
		}
	}

	for _, blockHash := range pruningPointAndItsAnticone {
		if blockHash.String() == "44ab153cad5e8ccfae0aa8d86ca5b1d7b702cc5fa089d4e502bd1c46508c19a7" {
			panic("omg")
		}
		block, err := tc.GetBlock(blockHash)
		if err != nil {
			return err
		}

		ghostdagData, err := tc.GHOSTDAGDataStore().Get(tc.DatabaseContext(), model.NewStagingArea(), blockHash, false)
		if err != nil {
			return err
		}

		err = w.Encode(jsonBlockWithTrustedData{
			Block:    domainBlockToRPCBlockWithHash(block),
			GHOSTDAG: ghostdagToJSON(ghostdagData),
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func writePPUTXO(tc testapi.TestConsensus) error {
	f, err := os.OpenFile(path.Join(mainPath, "pp-utxo.json.gz"), os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	gzipWriter := gzip.NewWriter(f)
	defer gzipWriter.Close()

	w := json.NewEncoder(gzipWriter)

	pruningPoint, err := tc.PruningPoint()
	if err != nil {
		return err
	}

	var fromOutpoint *externalapi.DomainOutpoint
	//txID, err := transactionid.FromString("2ae5dc02d6af675f9cd21e153b5db75c736554f47e5d52be68e09922908e2052")
	//if err != nil {
	//	return err
	//}
	//bla := externalapi.DomainOutpoint{
	//	TransactionID: *txID,
	//	Index:         0,
	//}
	const step = 100_000
	for count := 0; ; count++ {
		fmt.Printf("writePPUTXO: step number %d\n", count)
		outpointAndUTXOEntryPairs, err := tc.GetPruningPointUTXOs(pruningPoint, fromOutpoint, step)
		if err != nil {
			return err
		}

		if len(outpointAndUTXOEntryPairs) == 0 {
			break
		}

		fromOutpoint = outpointAndUTXOEntryPairs[len(outpointAndUTXOEntryPairs)-1].Outpoint
		entries := make([]*appmessage.UTXOsByAddressesEntry, len(outpointAndUTXOEntryPairs))
		for i, pair := range outpointAndUTXOEntryPairs {
			//if *pair.Outpoint == bla {
			//	panic("HIIIII")
			//}
			entries[i] = &appmessage.UTXOsByAddressesEntry{
				Outpoint: &appmessage.RPCOutpoint{
					TransactionID: pair.Outpoint.TransactionID.String(),
					Index:         pair.Outpoint.Index,
				},
				UTXOEntry: &appmessage.RPCUTXOEntry{
					Amount:          pair.UTXOEntry.Amount(),
					ScriptPublicKey: &appmessage.RPCScriptPublicKey{Script: hex.EncodeToString(pair.UTXOEntry.ScriptPublicKey().Script), Version: pair.UTXOEntry.ScriptPublicKey().Version},
					BlockDAAScore:   pair.UTXOEntry.BlockDAAScore(),
					IsCoinbase:      pair.UTXOEntry.IsCoinbase(),
				},
			}
		}

		err = w.Encode(entries)
		if err != nil {
			return err
		}

		if len(outpointAndUTXOEntryPairs) < step {
			break
		}
	}

	return nil
}

func domainBlockToRPCBlockWithHash(block *externalapi.DomainBlock) *appmessage.RPCBlock {
	rpcBlock := appmessage.DomainBlockToRPCBlock(block)
	rpcBlock.VerboseData = &appmessage.RPCBlockVerboseData{Hash: consensushashing.BlockHash(block).String()}
	return rpcBlock
}

func ghostdagToJSON(data *externalapi.BlockGHOSTDAGData) *jsonGHOSTDAGData {
	bluesAnticoneSizes := make([]*jsonBluesAnticoneSizes, 0, len(data.BluesAnticoneSizes()))
	for blueHsh, anticoneSize := range data.BluesAnticoneSizes() {
		bluesAnticoneSizes = append(bluesAnticoneSizes, &jsonBluesAnticoneSizes{
			BlueHash:     blueHsh.String(),
			AnticoneSize: anticoneSize,
		})
	}
	return &jsonGHOSTDAGData{
		BlueScore:          data.BlueScore(),
		BlueWork:           data.BlueWork().Text(16),
		SelectedParent:     data.SelectedParent().String(),
		MergeSetBlues:      hashesToStrings(data.MergeSetBlues()),
		MergeSetReds:       hashesToStrings(data.MergeSetReds()),
		BluesAnticoneSizes: bluesAnticoneSizes,
	}
}

func hashesToStrings(arr []*externalapi.DomainHash) []string {
	var strArr = make([]string, len(arr))
	for i, hash := range arr {
		strArr[i] = string(hash.String())
	}
	return strArr
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
