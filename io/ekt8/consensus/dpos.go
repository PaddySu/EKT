package consensus

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	"xserver/x_http/x_resp"

	"github.com/EducationEKT/EKT/io/ekt8/MPTPlus"
	"github.com/EducationEKT/EKT/io/ekt8/blockchain"
	"github.com/EducationEKT/EKT/io/ekt8/conf"
	"github.com/EducationEKT/EKT/io/ekt8/core/common"
	"github.com/EducationEKT/EKT/io/ekt8/db"
	"github.com/EducationEKT/EKT/io/ekt8/i_consensus"
	"github.com/EducationEKT/EKT/io/ekt8/log"
	"github.com/EducationEKT/EKT/io/ekt8/p2p"
	"github.com/EducationEKT/EKT/io/ekt8/util"

	"sync"
	"time"
)

type DPOSConsensus struct {
	Blockchain *blockchain.BlockChain
	Locker     sync.RWMutex
}

//从网络层转发过来的交易,进入打包流程
func (dpos DPOSConsensus) NewTransaction(tx common.Transaction) {
	dpos.Blockchain.Locker.Lock()
	defer dpos.Blockchain.Locker.Unlock()
	lastBlock, _ := dpos.Blockchain.LastBlock()
	if dpos.Blockchain.Status == blockchain.OpenStatus {
		var account common.Account
		address, _ := hex.DecodeString(tx.From)
		if err := lastBlock.StatTree.GetInterfaceValue(address, &account); err != nil {
			if account.GetNonce()+1 < tx.Nonce {
			}
		}
	}
}

func (dpos DPOSConsensus) Run() {
	for {
		defer func() {
			if r := recover(); r != nil {
				log.GetLogInst().LogDebug(`Consensus occured an unknown error, recovered. %v`, r)
				log.GetLogInst().LogCrit(`Consensus occured an unknown error, recovered. %v`, r)
				fmt.Println(r)
			}
		}()
		dpos.RUN()
	}
}

func (dpos DPOSConsensus) DPoSRun() {
	interval := 50 * time.Millisecond
	for {
		log.GetLogInst().LogInfo(`Timer tick: is my turn?`)
		if dpos.Blockchain.CurrentBlock.Round.IsMyTurn() {
			dpos.Pack(dpos.Blockchain.CurrentHeight)
			log.GetLogInst().LogInfo("Yes.")
		} else {
			log.GetLogInst().LogInfo("No, sleeping %d nano second.", interval)
			time.Sleep(interval)
		}
	}
}

func (dpos DPOSConsensus) RUN() {
	// 从数据库中恢复当前节点已同步的区块
	fmt.Println("Recover data from local database.")
	dpos.RecoverFromDB()
	fmt.Printf("Local data recovered. Current height is %d.\n", dpos.Blockchain.CurrentHeight)

	//获取21个节点的集合
	peers := dpos.GetCurrentDPOSPeers()
WaitingNodes:
	for {
		fmt.Println("detecting alive nodes......")
		aliveCount := AliveDPoSPeerCount(peers, true)
		if aliveCount > len(peers)/2 {
			fmt.Println()
			break
		}
		if aliveCount == 0 {
			fmt.Println("There is no node alive.")
		}
		fmt.Println("The number of surviving nodes is less than half, waiting for other nodes to restart.")
		time.Sleep(3 * time.Second)
	}
	fmt.Println("Alive node more than half, continue.")

	fmt.Println("Synchronizing blockchain...")
	interval := 50 * time.Millisecond
	flag := false
	failCount := 0
	for height := dpos.Blockchain.CurrentHeight + 1; flag == false; {
		if dpos.SyncHeight(height) {
			fmt.Printf("Synchronizing block at height %d successed. \n", height)
			height++
			failCount = 0
		} else {
			if AliveDPoSPeerCount(peers, false) <= len(dpos.Blockchain.CurrentBlock.Round.Peers)/2 {
				goto WaitingNodes
			}
			failCount++
			// 如果区块同步失败，会重试三次，三次之后判断当前节点是否是DPoS节点，选择不同的同步策略
			if failCount >= 3 {
				// 如果当前节点是DPoS节点，则不再根据区块高度同步区块，而是通过投票结果来同步区块
				if dpos.Blockchain.CurrentBlock.Round.MyIndex() != -1 {
					flag = true
					dpos.DPoSRun()
				} else {
					interval = 3 * time.Second
				}
			}
		}
		time.Sleep(interval)
	}
}

// 共识向blockchain发送signal进行下一个区块的打包
func (dpos DPOSConsensus) Pack(height int64) {
	bc := dpos.Blockchain
	bc.PackSignal()
}

func (dpos DPOSConsensus) BlockMinedCallBack(block *blockchain.Block) {
	fmt.Println("Mined block, sending block to other dpos  peer.")
	fmt.Println(dpos.Blockchain.CurrentBlock.Round)
	for _, peer := range block.Round.Peers {
		url := fmt.Sprintf("http://%s:%d/block/api/newBlock", peer.Address, peer.Port)
		resp, err := util.HttpPost(url, block.Bytes())
		fmt.Println(string(resp), err)
	}
}

func (dpos DPOSConsensus) RecoverFromDB() {
	block, err := dpos.Blockchain.LastBlock()
	// 如果是第一次打开
	if err != nil || block == nil {
		// 将创世块写入数据库
		accounts := conf.EKTConfig.GenesisBlockAccounts
		block = &blockchain.Block{
			Height:       0,
			Nonce:        0,
			Fee:          dpos.Blockchain.Fee,
			TotalFee:     0,
			PreviousHash: nil,
			CurrentHash:  nil,
			BlockBody:    blockchain.NewBlockBody(0),
			Body:         nil,
			Round: &i_consensus.Round{
				Peers:        dpos.GetCurrentDPOSPeers(),
				CurrentIndex: -1,
			},
			Locker:    sync.RWMutex{},
			StatTree:  MPTPlus.NewMTP(db.GetDBInst()),
			StatRoot:  nil,
			TxTree:    MPTPlus.NewMTP(db.GetDBInst()),
			TxRoot:    nil,
			EventTree: MPTPlus.NewMTP(db.GetDBInst()),
			EventRoot: nil,
			TokenRoot: nil,
			TokenTree: MPTPlus.NewMTP(db.GetDBInst()),
		}
		for _, account := range accounts {
			block.InsertAccount(account)
		}
		dpos.Blockchain.SaveBlock(block)
	}
	dpos.Blockchain.CurrentHeight = block.Height
	dpos.Blockchain.CurrentBlock = block
	dpos.Blockchain.CurrentBody = nil
}

//获取存活的DPOS节点数量
func AliveDPoSPeerCount(peers p2p.Peers, print bool) int {
	count := 0
	for _, peer := range peers {
		if peer.IsAlive() {
			if print {
				fmt.Printf("Peer %s is alive, address: %s \n", peer.PeerId, peer.Address)
			}
			count++
		}
	}
	return count
}

func (dpos DPOSConsensus) SyncHeight(height int64) bool {
	fmt.Printf("Synchronizing block at height %d \n", height)
	var header *blockchain.Block
	m := make(map[string]int)
	mapping := make(map[string]*blockchain.Block)
	for _, peer := range dpos.Blockchain.CurrentBlock.Round.Peers {
		block, err := getBlockHeader(peer, height)
		if err != nil {
			continue
		}
		mapping[hex.EncodeToString(block.Hash())] = block
		if _, ok := m[hex.EncodeToString(block.Hash())]; ok {
			m[hex.EncodeToString(block.Hash())]++
		} else {
			m[hex.EncodeToString(block.Hash())] = 1
		}
		if m[hex.EncodeToString(block.Hash())] > len(dpos.Blockchain.CurrentBlock.Round.Peers) {
			header = mapping[hex.EncodeToString(block.Hash())]
		}
	}
	if header == nil {
		return false
	}
	dpos.Blockchain.CurrentBlock = header
	dpos.Blockchain.CurrentHeight = header.Height
	fmt.Printf("Block at height %d header: %v \n", height, header)
	return true
}

func (dpos DPOSConsensus) pullBlock() {
	for {
		peers := dpos.Blockchain.CurrentBlock.Round.Peers
		for _, peer := range peers {
			block, _ := CurrentBlock(peer)
			if dpos.Blockchain.CurrentBlock.Height < block.Height {
			}
		}
	}
}

//从其他节点得到待验证block header
func (dpos DPOSConsensus) CurrentBlock() *blockchain.Block {
	var currentBlock *blockchain.Block = nil
	blocks := make(map[string]int64)
	mapping := make(map[string]*blockchain.Block)
	for _, peer := range dpos.Blockchain.CurrentBlock.Round.Peers {
		block, err := CurrentBlock(peer)
		if err != nil {
			continue
		}
		mapping[hex.EncodeToString(block.Hash())] = block
		num, exist := blocks[hex.EncodeToString(block.Hash())]
		if exist && num+1 >= int64(len(dpos.Blockchain.CurrentBlock.Round.Peers))/2 {
			currentBlock = block
			break
		} else {
			if exist {
				blocks[hex.EncodeToString(block.Hash())] = num + 1
			} else {
				blocks[hex.EncodeToString(block.Hash())] = 1
			}
		}
	}
	var maxNum int64 = 0
	var consensusHash string
	if currentBlock == nil {
		for hash, num := range blocks {
			if num > maxNum {
				maxNum, consensusHash = num, hash
			}
		}
	}
	return mapping[consensusHash]
}

//获取当前的peers
func (dpos DPOSConsensus) GetCurrentDPOSPeers() p2p.Peers {
	return p2p.MainChainDPosNode
}

func CurrentHeight(peer p2p.Peer) (int64, error) {
	url := fmt.Sprintf(`http://%s:%d/blocks/api/last`, peer.Address, peer.Port)
	body, err := util.HttpGet(url)
	if err != nil {
		return -1, err
	}
	var block blockchain.Block
	err = json.Unmarshal(body, &block)
	return block.Height, err
}

//向指定节点获取最新区块
func CurrentBlock(peer p2p.Peer) (*blockchain.Block, error) {
	url := fmt.Sprintf(`http://%s:%d/blocks/api/last`, peer.Address, peer.Port)
	body, err := util.HttpGet(url)
	if err != nil {
		return nil, err
	}
	return blockchain.FromBytes2Block(body)
}

func getBlockHeader(peer p2p.Peer, height int64) (*blockchain.Block, error) {
	url := fmt.Sprintf(`http://%s:%d/block/api/blockByHeight?height=%d`, peer.Address, peer.Port, height)
	body, err := util.HttpGet(url)
	if err != nil {
		return nil, err
	}
	var resp x_resp.XRespBody
	err = json.Unmarshal(body, &resp)
	data, err := json.Marshal(resp.Result)
	if err == nil {
		var block blockchain.Block
		err = json.Unmarshal(data, &block)
		return &block, err
	}
	return nil, err
}

func getBlockBody(peer p2p.Peer, height int64) (*blockchain.BlockBody, error) {
	url := fmt.Sprintf(`http://%s:%d/block/api/body?height=%d`, peer.Address, peer.Port, height)
	body, err := util.HttpGet(url)
	if err != nil {
		return nil, err
	}
	var resp x_resp.XRespBody
	err = json.Unmarshal(body, &resp)
	data, err := json.Marshal(resp.Result)
	if err == nil {
		var blockBody blockchain.BlockBody
		err = json.Unmarshal(data, &blockBody)
		return &blockBody, err
	}
	return nil, err
}
