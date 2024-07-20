// Copyright 2015 The etcd Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package raft

import (
	"errors"
	"math/rand"
	"time"

	pb "github.com/pingcap-incubator/tinykv/proto/pkg/eraftpb"
)

// None is a placeholder node ID used when there is no leader.
const None uint64 = 0

// StateType represents the role of a node in a cluster.
type StateType uint64

const (
	StateFollower StateType = iota
	StateCandidate
	StateLeader
)

var stmap = [...]string{
	"StateFollower",
	"StateCandidate",
	"StateLeader",
}

func (st StateType) String() string {
	return stmap[uint64(st)]
}

// ErrProposalDropped is returned when the proposal is ignored by some cases,
// so that the proposer can be notified and fail fast.
var ErrProposalDropped = errors.New("raft proposal dropped")

// Config contains the parameters to start a raft.
type Config struct {
	// ID is the identity of the local raft. ID cannot be 0.
	ID uint64

	// peers contains the IDs of all nodes (including self) in the raft cluster. It
	// should only be set when starting a new raft cluster. Restarting raft from
	// previous configuration will panic if peers is set. peer is private and only
	// used for testing right now.
	peers []uint64

	// ElectionTick is the number of Node.Tick invocations that must pass between
	// elections. That is, if a follower does not receive any message from the
	// leader of current term before ElectionTick has elapsed, it will become
	// candidate and start an election. ElectionTick must be greater than
	// HeartbeatTick. We suggest ElectionTick = 10 * HeartbeatTick to avoid
	// unnecessary leader switching.
	ElectionTick int
	// HeartbeatTick is the number of Node.Tick invocations that must pass between
	// heartbeats. That is, a leader sends heartbeat messages to maintain its
	// leadership every HeartbeatTick ticks.
	HeartbeatTick int

	// Storage is the storage for raft. raft generates entries and states to be
	// stored in storage. raft reads the persisted entries and states out of
	// Storage when it needs. raft reads out the previous state and configuration
	// out of storage when restarting.
	Storage Storage
	// Applied is the last applied index. It should only be set when restarting
	// raft. raft will not return entries to the application smaller or equal to
	// Applied. If Applied is unset when restarting, raft might return previous
	// applied entries. This is a very application dependent configuration.
	Applied uint64
}

func (c *Config) validate() error {
	if c.ID == None {
		return errors.New("cannot use none as id")
	}

	if c.HeartbeatTick <= 0 {
		return errors.New("heartbeat tick must be greater than 0")
	}

	if c.ElectionTick <= c.HeartbeatTick {
		return errors.New("election tick must be greater than heartbeat tick")
	}

	if c.Storage == nil {
		return errors.New("storage cannot be nil")
	}

	return nil
}

// Progress represents a follower’s progress in the view of the leader. Leader maintains
// progresses of all followers, and sends entries to the follower based on its progress.
type Progress struct {
	Match, Next uint64
}

type Raft struct {
	id uint64

	Term uint64
	Vote uint64
	// 多少人给我投票了
	Favors uint64
	// 多少人拒绝我了
	Rejects uint64
	// the log
	RaftLog *RaftLog

	// log replication progress of each peers
	Prs map[uint64]*Progress

	// this peer's role
	State StateType

	// votes records
	votes map[uint64]bool

	// msgs need to send
	msgs []pb.Message

	// the leader id
	Lead uint64

	// heartbeat interval, should send
	heartbeatTimeout int
	// baseline of election interval
	electionTimeout int
	// 实际的 election interval 值
	electionActual int
	// number of ticks since it reached last heartbeatTimeout.
	// only leader keeps heartbeatElapsed.
	heartbeatElapsed int
	// Ticks since it reached last electionTimeout when it is leader or candidate.
	// Number of ticks since it reached last electionTimeout or received a
	// valid message from current leader when it is a follower.
	electionElapsed int

	// leadTransferee is id of the leader transfer target when its value is not zero.
	// Follow the procedure defined in section 3.10 of Raft phd thesis.
	// (https://web.stanford.edu/~ouster/cgi-bin/papers/OngaroPhD.pdf)
	// (Used in 3A leader transfer)
	leadTransferee uint64

	// Only one conf change may be pending (in the log, but not yet
	// applied) at a time. This is enforced via PendingConfIndex, which
	// is set to a value >= the log index of the latest pending
	// configuration change (if any). Config changes are only allowed to
	// be proposed if the leader's applied index is greater than this
	// value.
	// (Used in 3A conf change)
	PendingConfIndex uint64
}

// newRaft return a raft peer with the given config
func newRaft(c *Config) *Raft {
	if err := c.validate(); err != nil {
		panic(err.Error())
	}
	// Your Code Here (2A).
	prs := make(map[uint64]*Progress)
	Votes := make(map[uint64]bool)
	for _, pr := range c.peers {
		prs[pr] = &Progress{}
		Votes[pr] = false
	}
	rand.Seed(time.Now().UnixNano())
	tk := c.ElectionTick

	hardState, _, _ := c.Storage.InitialState()
	return &Raft{
		id:               c.ID,
		votes:            Votes,
		Term:             hardState.Term,
		Vote:             hardState.Vote,
		Prs:              prs,
		heartbeatTimeout: c.HeartbeatTick,
		electionTimeout:  tk,
		electionActual:   tk + 1 + rand.Intn(tk-1),
		RaftLog:          newLog(c.Storage),
	}
}

// sendAppend sends an append RPC with new entries (if any) and the
// current commit index to the given peer. Returns true if a message was sent.
func (r *Raft) sendAppend(to uint64) bool {
	// Your Code Here (2A).
	entries := make([]*pb.Entry, 0, r.RaftLog.LastIndex()-r.RaftLog.committed)
	for index := r.RaftLog.committed + 1; index <= r.RaftLog.LastIndex(); index++ {
		entries = append(entries, &r.RaftLog.entries[index])
	}
	r.msgs = append(r.msgs, pb.Message{
		From:    r.id,
		To:      to,
		MsgType: pb.MessageType_MsgAppend,
		Term:    r.Term,
		Index:   r.RaftLog.committed, // 发送的是没有 commit 的日志
		Entries: entries,
	})
	return true
}

// sendHeartbeat sends a heartbeat RPC to the given peer.
func (r *Raft) sendHeartbeat(to uint64) {
	// Your Code Here (2A).
	r.msgs = append(r.msgs, pb.Message{
		From:    r.id,
		To:      to,
		Term:    r.Term,
		MsgType: pb.MessageType_MsgHeartbeat,
	})
}

// tick advances the internal logical clock by a single tick.
func (r *Raft) tick() {
	// Your Code Here (2A).
	switch r.State {
	case StateFollower:
		r.electionElapsed++
		if r.electionElapsed == r.electionActual {
			r.becomeCandidate()
			r.readMsgs() // 发出新的请求之前，清空原有的请求
			r.sendVoteRequestToAll()
		}
	case StateCandidate:
		r.electionElapsed++
		if r.electionElapsed == r.electionActual {
			r.becomeCandidate()
			r.readMsgs() // 发出新的请求之前，清空原有的请求
			r.sendVoteRequestToAll()
		}
	case StateLeader:
		r.heartbeatElapsed++
		// 心该跳的时候就跳，向其他 nodes 发送心跳消息
		if r.heartbeatElapsed == r.heartbeatTimeout {
			r.heartbeatElapsed = 0
			for pr := range r.Prs {
				if pr != r.id {
					r.sendHeartbeat(pr)
				}
			}
		}
	}
}

// becomeFollower transform this peer's state to Follower
func (r *Raft) becomeFollower(term uint64, lead uint64) {
	// Your Code Here (2A).
	r.Term = term
	r.Lead = lead
	r.State = StateFollower
	r.Vote = None         // 重置选票
	r.electionElapsed = 0 // 选举时间回满
	r.electionActual = r.electionTimeout + 1 + rand.Intn(r.electionTimeout-1)
}

// becomeCandidate transform this peer's state to candidate
func (r *Raft) becomeCandidate() {
	// Your Code Here (2A).
	r.State = StateCandidate
	r.Term++
	r.electionElapsed = 0
	r.electionActual = r.electionTimeout + 1 + rand.Intn(r.electionTimeout-1)
	r.Favors = 1
	r.Rejects = 0
	r.votes[r.id] = true // 我选我自己
}

// becomeLeader transform this peer's state to leader
func (r *Raft) becomeLeader() {
	// Your Code Here (2A).
	// NOTE: Leader should propose a noop entry on its term
	r.State = StateLeader
	r.Lead = r.id
	r.heartbeatElapsed = 0 //开始心跳
	// noop entry
	r.RaftLog.entries = append(r.RaftLog.entries, pb.Entry{
		Term:  r.Term,
		Index: r.RaftLog.entries[0].Index + r.RaftLog.LastIndex() + 1,
	})
	r.setLast(r.id, r.RaftLog.LastIndex(), uint64(len(r.Prs)))
	r.Prs[r.id].Match, r.Prs[r.id].Next = r.RaftLog.LastIndex(), r.RaftLog.LastIndex()+1
}

// Step the entrance of handle message, see `MessageType`
// on `eraftpb.proto` for what msgs should be handled
func (r *Raft) Step(m pb.Message) error {
	// Your Code Here (2A).
	if r.Term < m.Term {
		// 本地信息可以省略 Term 的。
		r.becomeFollower(m.Term, None) // 老旧的任期将被淘汰，并变成 Follower，暂时不知道领导者是谁
	}
	if m.MsgType == pb.MessageType_MsgAppend {
		if m.Term >= r.Term {
			r.becomeFollower(m.Term, m.From)
		}
	}
	switch r.State {
	case StateFollower:
		switch m.MsgType {
		case pb.MessageType_MsgHup:
			r.becomeCandidate()
			r.readMsgs() // 发出新的请求之前，清空原有的请求
			r.sendVoteRequestToAll()
			r.Lead = 0
			if 2*r.Favors > uint64(len(r.Prs)) {
				r.becomeLeader()
				r.Step(pb.Message{From: r.id, To: r.id, MsgType: pb.MessageType_MsgPropose})
				// r.msgs = append(r.msgs, pb.Message{From: r.id, To: r.id, MsgType: pb.MessageType_MsgPropose})
			} else if 2*r.Rejects >= uint64(len(r.Prs)) {
				r.becomeFollower(r.Term, r.Lead)
			}
		case pb.MessageType_MsgRequestVote:
			reject := true
			if (r.Vote == None || r.Vote == m.From) && (r.RaftLog.LastTerm() < m.LogTerm || r.RaftLog.LastTerm() == m.LogTerm && r.RaftLog.LastIndex() <= m.Index) {
				reject = false
				r.Vote = m.From // 不予拒绝
			}
			// if reject {
			// 	fmt.Println("Rejected:", r.Term, m.From, m.To, r.RaftLog.LastTerm(), m.LogTerm)
			// }
			r.msgs = append(r.msgs, pb.Message{
				From:    r.id,
				To:      m.From,
				Term:    r.Term,
				MsgType: pb.MessageType_MsgRequestVoteResponse,
				Reject:  reject,
			})
		case pb.MessageType_MsgHeartbeat:
			r.handleHeartbeat(m)
		case pb.MessageType_MsgAppend:
			r.handleAppendEntries(m)
		}

	case StateCandidate:
		switch m.MsgType {
		case pb.MessageType_MsgHup:
			r.becomeCandidate()
			r.readMsgs() // 发出新的请求之前，清空原有的请求
			for pr := range r.Prs {
				if pr != r.id {
					r.msgs = append(r.msgs, pb.Message{
						From:    r.id,
						To:      pr,
						Term:    r.Term,
						MsgType: pb.MessageType_MsgRequestVote,
						Index:   r.RaftLog.LastIndex(),
						LogTerm: r.RaftLog.LastTerm(),
					})
				}
			}
			r.Lead = 0
		case pb.MessageType_MsgRequestVote:
			// Candidate 给自己投票，不给别的 Candidate 投票
			r.msgs = append(r.msgs, pb.Message{
				From:    r.id,
				To:      m.From,
				Term:    r.Term,
				MsgType: pb.MessageType_MsgRequestVoteResponse,
				Reject:  true, // 拒绝
			})
		case pb.MessageType_MsgRequestVoteResponse:
			if m.To == r.id {
				if m.Reject {
					r.Rejects++
				} else {
					r.Favors++
				}
			}
			if 2*r.Favors > uint64(len(r.Prs)) {
				r.becomeLeader()
				r.msgs = append(r.msgs, pb.Message{From: r.id, To: r.id, MsgType: pb.MessageType_MsgPropose})
			} else if 2*r.Rejects >= uint64(len(r.Prs)) {
				r.becomeFollower(r.Term, r.Lead)
			}
		case pb.MessageType_MsgHeartbeat:
			r.handleHeartbeat(m)
		}
	case StateLeader:
		switch m.MsgType {
		case pb.MessageType_MsgRequestVote:
			r.msgs = append(r.msgs, pb.Message{
				From:    r.id,
				To:      m.From,
				Term:    r.Term,
				MsgType: pb.MessageType_MsgRequestVoteResponse,
				Reject:  true, // 拒绝
			})
		case pb.MessageType_MsgBeat:
			for pr := range r.Prs {
				if pr != r.id {
					r.sendHeartbeat(pr)
				}
			}
		case pb.MessageType_MsgHeartbeatResponse:
			// 发现跟随者有老旧的日志，发送更新的 rpc 请求
			if m.LogTerm < r.RaftLog.LastTerm() || m.LogTerm == r.RaftLog.LastTerm() && m.Index < r.RaftLog.LastIndex() {
				entries := make([]*pb.Entry, 0, uint64(len(r.RaftLog.entries)))
				for index := m.Index; index < uint64(len(r.RaftLog.entries)); index++ {
					entries = append(entries, &r.RaftLog.entries[index])
				}
				r.msgs = append(r.msgs, pb.Message{
					From:    r.id,
					To:      m.From,
					Term:    r.Term,
					MsgType: pb.MessageType_MsgAppend,
					Index:   m.Index,
					LogTerm: m.LogTerm,
					Commit:  r.RaftLog.committed,
					Entries: entries,
				})
			}
		case pb.MessageType_MsgPropose:
			for index, entry := range m.Entries {
				entry.Term = r.Term
				entry.Index = r.RaftLog.LastIndex() + 1 + uint64(index)
			}
			for pr := range r.Prs {
				if pr != r.id {
					r.msgs = append(r.msgs, pb.Message{
						From:    r.id,
						To:      pr,
						Term:    r.Term,
						MsgType: pb.MessageType_MsgAppend,
						Index:   r.RaftLog.LastIndex(),
						LogTerm: r.RaftLog.LastTerm(),
						Commit:  r.RaftLog.committed,
						Entries: m.Entries,
					})
				}
			}
			for _, entries := range m.Entries {
				r.RaftLog.entries = append(r.RaftLog.entries, *entries)
			}
			r.setLast(r.id, r.RaftLog.LastIndex(), uint64(len(r.Prs)))
			r.Prs[r.id].Match, r.Prs[r.id].Next = r.RaftLog.LastIndex(), r.RaftLog.LastIndex()+1
		case pb.MessageType_MsgAppendResponse:
			if m.To == r.id {
				if !m.Reject {
					committed := r.RaftLog.committed
					r.setLast(m.From, m.Index, uint64(len(r.Prs)))
					if committed != r.RaftLog.committed {
						// committed 更新了，立刻发布给跟随者
						for pr := range r.Prs {
							if pr != r.id {
								r.msgs = append(r.msgs, pb.Message{
									From:    r.id,
									To:      pr,
									Term:    r.Term,
									MsgType: pb.MessageType_MsgAppend,
									Commit:  r.RaftLog.committed,
								})
							}
						}

					}
				} else {
					// 减小 index 重新发送 append rpc 请求
					entries := make([]*pb.Entry, 0)
					for idx := m.Index - 1; idx < uint64(len(r.RaftLog.entries)); idx++ {
						entries = append(entries, &r.RaftLog.entries[idx])
					}
					r.msgs = append(r.msgs, pb.Message{
						From:    r.id,
						To:      m.From,
						Term:    r.Term,
						MsgType: pb.MessageType_MsgAppend,
						Index:   m.Index - 1,
						LogTerm: entries[0].Term,
						Commit:  r.RaftLog.committed,
						Entries: entries,
					})
				}
			}
		}

	}
	return nil
}

// 更新领导者的易失存储中 peer 号节点的日志的 LastIndex 值为 newLast
func (r *Raft) setLast(peer uint64, newLast uint64, peers uint64) {
	r.Prs[peer].Match, r.Prs[peer].Next = newLast, newLast+1
	l := r.RaftLog
	l.indexCount[l.peerLast[peer]]--
	l.peerLast[peer] = newLast
	l.indexCount[l.peerLast[peer]]++
	count := uint64(0)
	currentTerm := l.LastTerm() // 只会提交当前任期的日志
	for index := l.LastIndex(); index > l.committed && l.entries[index].Term == currentTerm; index-- {
		count += l.indexCount[index]
		if 2*count > peers {
			l.committed = index
			return
		}
	}
}

// handleAppendEntries handle AppendEntries RPC request
func (r *Raft) handleAppendEntries(m pb.Message) {
	// Your Code Here (2A).
	rejectMessage := pb.Message{
		From:    r.id,
		To:      m.From,
		Term:    m.Term,
		MsgType: pb.MessageType_MsgAppendResponse,
		Index:   m.Index,
		Reject:  true,
	}
	// 拒收老旧的消息
	if m.Term == r.Term {
		haveEntry, _ := r.haveEntry(m.Index, m.LogTerm)
		if !haveEntry {
			r.msgs = append(r.msgs, rejectMessage)
			// fmt.Println("rejectWithindex,logTerm:", m.Index, m.LogTerm) // ???????????????
			return
		}
		for index := 0; index < len(m.Entries); index++ {
			term, err := r.RaftLog.Term(m.Entries[index].Index)
			if err != nil || term != m.Entries[index].Term {
				r.RaftLog.stabled = m.Entries[index].Index - 1
				r.RaftLog.storage.(*MemoryStorage).Append(r.RaftLog.entries[:m.Entries[index].Index])
				r.RaftLog.entries = r.RaftLog.entries[:m.Entries[index].Index]
				for ; index < len(m.Entries); index++ {
					r.RaftLog.entries = append(r.RaftLog.entries, *m.Entries[index])
				}
			}
		}
		if m.Commit > r.RaftLog.committed {
			if m.Entries == nil {
				if m.Index == 0 {
					r.RaftLog.committed = m.Commit
				} else {
					r.RaftLog.committed = min(m.Commit, m.Index)
				}
			} else {
				r.RaftLog.committed = min(m.Commit, m.Entries[len(m.Entries)-1].Index)
			}
		}
		r.msgs = append(r.msgs, pb.Message{
			From:    r.id,
			To:      m.From,
			MsgType: pb.MessageType_MsgAppendResponse,
			Term:    m.Term,
			Index:   r.RaftLog.LastIndex(),
		})
	} else {
		// fmt.Println("Unexpected surprise")
		r.msgs = append(r.msgs, rejectMessage)
	}
}

// 判断一个 Raft 是否拥有对应的 entry，并返回位置
func (r *Raft) haveEntry(index, term uint64) (bool, uint64) {
	if index == 0 && term == 0 {
		return true, 0
	}
	for idx, entry := range r.RaftLog.allEntries() {
		if entry.Term == term && entry.Index == index {
			return true, uint64(idx + 1)
		}
	}
	return false, 0
}

// handleHeartbeat handle Heartbeat RPC request
func (r *Raft) handleHeartbeat(m pb.Message) {
	// Your Code Here (2A).
	if m.To == r.id {
		r.becomeFollower(m.Term, m.From)
		r.msgs = append(r.msgs, pb.Message{
			From:    r.id,
			To:      m.From,
			MsgType: pb.MessageType_MsgHeartbeatResponse,
			Term:    r.Term,
			Index:   r.RaftLog.LastIndex(),
			LogTerm: r.RaftLog.LastTerm(),
		})
	}
}

func (r *Raft) sendVoteRequestToAll() {
	for pr := range r.Prs {
		if pr != r.id {
			r.msgs = append(r.msgs, pb.Message{
				From:    r.id,
				To:      pr,
				Term:    r.Term,
				MsgType: pb.MessageType_MsgRequestVote,
				Index:   r.RaftLog.LastIndex(),
				LogTerm: r.RaftLog.LastTerm(),
			})
		}
	}
}

// handleSnapshot handle Snapshot RPC request
func (r *Raft) handleSnapshot(m pb.Message) {
	// Your Code Here (2C).
}

// addNode add a new node to raft group
func (r *Raft) addNode(id uint64) {
	// Your Code Here (3A).
}

// removeNode remove a node from raft group
func (r *Raft) removeNode(id uint64) {
	// Your Code Here (3A).
}

func (r *Raft) readMsgs() []pb.Message {
	msgs := r.msgs
	r.msgs = make([]pb.Message, 0)

	return msgs
}
