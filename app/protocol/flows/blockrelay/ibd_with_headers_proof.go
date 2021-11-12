package blockrelay

import (
	"fmt"
	"github.com/kaspanet/kaspad/app/appmessage"
	"github.com/kaspanet/kaspad/app/protocol/common"
	"github.com/kaspanet/kaspad/app/protocol/protocolerrors"
	"github.com/kaspanet/kaspad/domain/consensus/model/externalapi"
	"github.com/kaspanet/kaspad/domain/consensus/ruleerrors"
	"github.com/kaspanet/kaspad/domain/consensus/utils/consensushashing"
	"github.com/pkg/errors"
)

func (flow *handleRelayInvsFlow) ibdWithHeadersProof(highHash *externalapi.DomainHash) error {
	err := flow.Domain().InitStagingConsensus()
	if err != nil {
		return err
	}

	err = flow.downloadHeadersAndPruningUTXOSet(highHash)
	if err != nil {
		if !flow.IsRecoverableError(err) {
			return err
		}

		deleteStagingConsensusErr := flow.Domain().DeleteStagingConsensus()
		if deleteStagingConsensusErr != nil {
			return deleteStagingConsensusErr
		}

		return err
	}

	err = flow.Domain().CommitStagingConsensus()
	if err != nil {
		return err
	}

	//panic("here")

	return nil
}

func (flow *handleRelayInvsFlow) shouldSyncAndShouldDownloadHeadersProof(highBlock *externalapi.DomainBlock,
	highestSharedBlockFound bool) (shouldDownload, shouldSync bool, err error) {

	if !highestSharedBlockFound {
		hasMoreBlueWorkThanSelectedTipAndPruningDepthMoreBlueScore, err := flow.checkIfHighHashHasMoreBlueWorkThanSelectedTipAndPruningDepthMoreBlueScore(highBlock)
		if err != nil {
			return false, false, err
		}

		if hasMoreBlueWorkThanSelectedTipAndPruningDepthMoreBlueScore {
			return true, true, nil
		}

		return false, false, nil
	}

	return false, true, nil
}

func (flow *handleRelayInvsFlow) checkIfHighHashHasMoreBlueWorkThanSelectedTipAndPruningDepthMoreBlueScore(highBlock *externalapi.DomainBlock) (bool, error) {
	headersSelectedTip, err := flow.Domain().Consensus().GetHeadersSelectedTip()
	if err != nil {
		return false, err
	}

	headersSelectedTipInfo, err := flow.Domain().Consensus().GetBlockInfo(headersSelectedTip)
	if err != nil {
		return false, err
	}

	if highBlock.Header.BlueScore() < headersSelectedTipInfo.BlueScore+flow.Config().NetParams().PruningDepth() {
		return false, nil
	}

	return highBlock.Header.BlueWork().Cmp(headersSelectedTipInfo.BlueWork) > 0, nil
}

func (flow *handleRelayInvsFlow) downloadPruningPointProof() (*externalapi.PruningPointProof, error) {
	log.Infof("Downloading the pruning point proof from %s", flow.peer)
	err := flow.outgoingRoute.Enqueue(appmessage.NewMsgRequestPruningPointProof())
	if err != nil {
		return nil, err
	}
	message, err := flow.dequeueIncomingMessageAndSkipInvs(common.DefaultTimeout)
	if err != nil {
		return nil, err
	}
	pruningPointProofMessage, ok := message.(*appmessage.MsgPruningPointProof)
	if !ok {
		return nil, protocolerrors.Errorf(true, "received unexpected message type. "+
			"expected: %s, got: %s", appmessage.CmdPruningPointProof, message.Command())
	}
	return appmessage.MsgPruningPointProofToDomainPruningPointProof(pruningPointProofMessage), nil
}

func (flow *handleRelayInvsFlow) validatePruningPointProof(pruningPointProof *externalapi.PruningPointProof) (*externalapi.DomainHash, error) {
	err := flow.Domain().Consensus().ValidatePruningPointProof(pruningPointProof)
	if err != nil {
		if errors.As(err, &ruleerrors.RuleError{}) {
			return nil, protocolerrors.Wrapf(true, err, "pruning point proof validation failed")
		}
		return nil, err
	}

	err = flow.Domain().StagingConsensus().ApplyPruningPointProof(pruningPointProof)
	if err != nil {
		return nil, err
	}

	return consensushashing.HeaderHash(pruningPointProof.Headers[0][len(pruningPointProof.Headers[0])-1]), nil
}

func (flow *handleRelayInvsFlow) downloadHeadersAndPruningUTXOSet(highHash *externalapi.DomainHash) error {
	pruningPointProof, err := flow.downloadPruningPointProof()
	if err != nil {
		return err
	}

	proofPruningPoint, err := flow.validatePruningPointProof(pruningPointProof)
	if err != nil {
		return err
	}

	pruningPoints, blocksWithTrustedData, err := flow.syncPruningPointsAndPruningPointAnticone(proofPruningPoint)
	if err != nil {
		return err
	}

	// TODO: Remove this condition once there's more proper way to check finality violation
	// in the headers proof.
	if proofPruningPoint.Equal(flow.Config().NetParams().GenesisHash) {
		return protocolerrors.Errorf(true, "the genesis pruning point violates finality")
	}

	err = flow.syncPruningPointFutureHeaders(flow.Domain().StagingConsensus(), proofPruningPoint, highHash, true)
	if err != nil {
		return err
	}

	log.Debugf("Headers downloaded from peer %s", flow.peer)

	highHashInfo, err := flow.Domain().StagingConsensus().GetBlockInfo(highHash)
	if err != nil {
		return err
	}

	if !highHashInfo.Exists {
		return protocolerrors.Errorf(true, "the triggering IBD block was not sent")
	}

	err = flow.validatePruningPointFutureHeaderTimestamps()
	if err != nil {
		return err
	}

	//log.Infof("Checking if the suggested pruning point %s is compatible to the node DAG", proofPruningPoint)
	//isValid, err := flow.Domain().StagingConsensus().IsValidPruningPoint(proofPruningPoint)
	//if err != nil {
	//	return err
	//}
	//
	//if !isValid {
	//	return protocolerrors.Errorf(true, "invalid pruning point %s", proofPruningPoint)
	//}

	err = flow.Domain().DeleteStagingConsensus()
	if err != nil {
		return err
	}

	err = flow.Domain().InitStagingConsensus()
	if err != nil {
		return err
	}

	err = flow.Domain().StagingConsensus().ApplyPruningPointProof(pruningPointProof)
	if err != nil {
		return err
	}

	err = flow.Domain().StagingConsensus().ImportPruningPoints(pruningPoints)
	if err != nil {
		return err
	}

	for i, block := range blocksWithTrustedData {
		log.Criticalf("TRUSTED %d %s", i, block.Block.Header.BlockHash())
		err := flow.processBlockWithTrustedData(flow.Domain().StagingConsensus(), block)
		if err != nil {
			return err
		}
	}

	return nil
}

func (flow *handleRelayInvsFlow) syncPruningPointsAndPruningPointAnticone(proofPruningPoint *externalapi.DomainHash) ([]externalapi.BlockHeader, []*appmessage.MsgBlockWithTrustedData, error) {
	log.Infof("Downloading the past pruning points and the pruning point anticone from %s", flow.peer)
	err := flow.outgoingRoute.Enqueue(appmessage.NewMsgRequestPruningPointAndItsAnticone())
	if err != nil {
		return nil, nil, err
	}

	pruningPoints, err := flow.validateAndInsertPruningPoints(proofPruningPoint)
	if err != nil {
		return nil, nil, err
	}

	pruningPointWithMetaData, done, err := flow.receiveBlockWithTrustedData()
	if err != nil {
		return nil, nil, err
	}
	if done {
		return nil, nil, protocolerrors.Errorf(true, "got `done` message before receiving the pruning point")
	}

	if !pruningPointWithMetaData.Block.Header.BlockHash().Equal(proofPruningPoint) {
		return nil, nil, protocolerrors.Errorf(true, "first block with trusted data is not the pruning point")
	}

	err = flow.processBlockWithTrustedData(flow.Domain().StagingConsensus(), pruningPointWithMetaData)
	if err != nil {
		return nil, nil, err
	}

	blocksWithTrustedData := make([]*appmessage.MsgBlockWithTrustedData, 0, flow.Config().NetParams().K+1)
	blocksWithTrustedData = append(blocksWithTrustedData, pruningPointWithMetaData)

	for {
		blockWithTrustedData, done, err := flow.receiveBlockWithTrustedData()
		if err != nil {
			return nil, nil, err
		}

		if done {
			break
		}

		err = flow.processBlockWithTrustedData(flow.Domain().StagingConsensus(), blockWithTrustedData)
		if err != nil {
			return nil, nil, err
		}

		blocksWithTrustedData = append(blocksWithTrustedData, blockWithTrustedData)
	}

	log.Infof("Finished downloading pruning point and its anticone from %s", flow.peer)
	return pruningPoints, blocksWithTrustedData, nil
}

func (flow *handleRelayInvsFlow) processBlockWithTrustedData(
	consensus externalapi.Consensus, block *appmessage.MsgBlockWithTrustedData) error {

	_, err := consensus.ValidateAndInsertBlockWithTrustedData(appmessage.BlockWithTrustedDataToDomainBlockWithTrustedData(block), false)
	return err
}

func (flow *handleRelayInvsFlow) receiveBlockWithTrustedData() (*appmessage.MsgBlockWithTrustedData, bool, error) {
	message, err := flow.dequeueIncomingMessageAndSkipInvs(common.DefaultTimeout)
	if err != nil {
		return nil, false, err
	}

	switch downCastedMessage := message.(type) {
	case *appmessage.MsgBlockWithTrustedData:
		return downCastedMessage, false, nil
	case *appmessage.MsgDoneBlocksWithTrustedData:
		return nil, true, nil
	default:
		return nil, false,
			protocolerrors.Errorf(true, "received unexpected message type. "+
				"expected: %s or %s, got: %s",
				(&appmessage.MsgBlockWithTrustedData{}).Command(),
				(&appmessage.MsgDoneBlocksWithTrustedData{}).Command(),
				downCastedMessage.Command())
	}
}

func (flow *handleRelayInvsFlow) receivePruningPoints() (*appmessage.MsgPruningPoints, error) {
	message, err := flow.dequeueIncomingMessageAndSkipInvs(common.DefaultTimeout)
	if err != nil {
		return nil, err
	}

	msgPruningPoints, ok := message.(*appmessage.MsgPruningPoints)
	if !ok {
		return nil,
			protocolerrors.Errorf(true, "received unexpected message type. "+
				"expected: %s, got: %s", appmessage.CmdPruningPoints, message.Command())
	}

	return msgPruningPoints, nil
}

func (flow *handleRelayInvsFlow) validateAndInsertPruningPoints(proofPruningPoint *externalapi.DomainHash) ([]externalapi.BlockHeader, error) {
	currentPruningPoint, err := flow.Domain().Consensus().PruningPoint()
	if err != nil {
		return nil, err
	}

	if currentPruningPoint.Equal(proofPruningPoint) {
		return nil, protocolerrors.Errorf(true, "the proposed pruning point is the same as the current pruning point")
	}

	pruningPoints, err := flow.receivePruningPoints()
	if err != nil {
		return nil, err
	}

	headers := make([]externalapi.BlockHeader, len(pruningPoints.Headers))
	for i, header := range pruningPoints.Headers {
		headers[i] = appmessage.BlockHeaderToDomainBlockHeader(header)
	}

	arePruningPointsViolatingFinality, err := flow.Domain().Consensus().ArePruningPointsViolatingFinality(headers)
	if err != nil {
		return nil, err
	}

	if arePruningPointsViolatingFinality {
		// TODO: Find a better way to deal with finality conflicts.
		return nil, protocolerrors.Errorf(false, "pruning points are violating finality")
	}

	lastPruningPoint := consensushashing.HeaderHash(headers[len(headers)-1])
	if !lastPruningPoint.Equal(proofPruningPoint) {
		return nil, protocolerrors.Errorf(true, "the proof pruning point is not equal to the last pruning "+
			"point in the list")
	}

	err = flow.Domain().StagingConsensus().ImportPruningPoints(headers)
	if err != nil {
		return nil, err
	}

	return headers, nil
}

func (flow *handleRelayInvsFlow) syncPruningPointUTXOSet(consensus externalapi.Consensus) (bool, error) {

	log.Info("Fetching the pruning point UTXO set")
	pruningPoint, err := consensus.PruningPoint()
	if err != nil {
		return false, err
	}

	isSuccessful, err := flow.fetchMissingUTXOSet(consensus, pruningPoint)
	if err != nil {
		return false, err
	}

	if !isSuccessful {
		log.Infof("Couldn't successfully fetch the pruning point UTXO set. Stopping IBD.")
		return false, nil
	}

	log.Info("Fetched the new pruning point UTXO set")
	return true, nil
}

func (flow *handleRelayInvsFlow) fetchMissingUTXOSet(consensus externalapi.Consensus, pruningPointHash *externalapi.DomainHash) (succeed bool, err error) {
	defer func() {
		err := consensus.ClearImportedPruningPointData()
		if err != nil {
			panic(fmt.Sprintf("failed to clear imported pruning point data: %s", err))
		}
	}()

	err = flow.outgoingRoute.Enqueue(appmessage.NewMsgRequestPruningPointUTXOSet(pruningPointHash))
	if err != nil {
		return false, err
	}

	receivedAll, err := flow.receiveAndInsertPruningPointUTXOSet(consensus, pruningPointHash)
	if err != nil {
		return false, err
	}
	if !receivedAll {
		return false, nil
	}

	err = consensus.ValidateAndInsertImportedPruningPoint(pruningPointHash)
	if err != nil {
		// TODO: Find a better way to deal with finality conflicts.
		if errors.Is(err, ruleerrors.ErrSuggestedPruningViolatesFinality) {
			return false, nil
		}
		return false, protocolerrors.ConvertToBanningProtocolErrorIfRuleError(err, "error with pruning point UTXO set")
	}

	err = flow.OnPruningPointUTXOSetOverride()
	if err != nil {
		return false, err
	}

	return true, nil
}
