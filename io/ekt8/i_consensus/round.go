package i_consensus

import (
	"encoding/json"
	"fmt"

	"github.com/EducationEKT/EKT/io/ekt8/conf"
	"github.com/EducationEKT/EKT/io/ekt8/p2p"
	"github.com/EducationEKT/EKT/io/ekt8/util"
)

type Round struct {
	CurrentIndex int        `json:"currentIndex"` // default -1
	Peers        []p2p.Peer `json:"peers"`
	Random       int        `json:"random"`
}

func (round1 *Round) Equal(round2 *Round) bool {
	if round1.CurrentIndex != round2.CurrentIndex || len(round1.Peers) != len(round2.Peers) {
		return false
	}
	for i, peer := range round1.Peers {
		if !peer.Equal(round2.Peers[i]) {
			return false
		}
	}
	return true
}

func (round *Round) IndexPlus(CurrentHash []byte) *Round {
	if round.CurrentIndex == len(round.Peers)-1 {
		Random := util.BytesToInt(CurrentHash[22:])
		round = &Round{
			CurrentIndex: 0,
			Peers:        round.Peers,
			Random:       Random,
		}
	} else {
		round.CurrentIndex++
	}
	return round
}

func (round *Round) NextRound(CurrentHash []byte) *Round {
	if round.CurrentIndex == len(round.Peers)-1 {
		bytes := CurrentHash[22:]
		Random := util.BytesToInt(bytes)
		round = &Round{
			CurrentIndex: round.MyIndex(),
			Peers:        round.Peers,
			Random:       Random,
		}
	} else {
		round.CurrentIndex = round.MyIndex()
	}
	return round
}

func (round Round) IsMyTurn() bool {
	if round.Peers[(round.CurrentIndex+1)%len(round.Peers)].Equal(conf.EKTConfig.Node) {
		return true
	}
	return false
}

func (round Round) MyIndex() int {
	for i, peer := range round.Peers {
		if peer.Equal(conf.EKTConfig.Node) {
			return i
		}
	}
	return -1
}

func (round Round) Len() int {
	return len(round.Peers)
}

func (round Round) Swap(i, j int) {
	round.Peers[i], round.Peers[j] = round.Peers[j], round.Peers[i]
}

func (round Round) Less(i, j int) bool {
	return round.Random%(i+j)%2 == 1
}

func (round Round) String() string {
	peers, _ := json.Marshal(round.Peers)
	return fmt.Sprintf(`{"currentIndex": %d, "peers": %s, "random": %d}`, round.CurrentIndex, string(peers), round.Random)
}
