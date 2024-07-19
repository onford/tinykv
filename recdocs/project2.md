# Project2 RaftKV

实验二是要根据 raft 算法建成一个集群数据库。raft 算法的思路非常清晰，根据其论文就可大致了解作用过程;加之动画演示，我对算法的原理了解更加深刻。该项目的完成思路包括：
- 查看论文，研究动画演示，辅助对代码功能的理解。
- 从测试集的说明或者测试过程入手，分析项目哪些功能没有完成，或者说实现过程有偏差，并且逐步完善。

## Project 2a
### Project 2aa
这一部分是 raft 算法基本原理的 go 实现，主要是完成`raft.go`中的代码。项目本身提供了一些结构体，这里给出我的理解。  
#### NetWork 的组织形式
首先是分布式数据库的基本骨架，一个`network`：
```go
type network struct {
	peers   map[uint64]stateMachine
	storage map[uint64]*MemoryStorage
	dropm   map[connem]float64
	ignorem map[pb.MessageType]bool

	// msgHook is called for each message sent. It may inspect the
	// message and return true to send it or false to drop it.
	msgHook func(pb.Message) bool
}
```
其中`peers`是网络中各个节点的编号到其状态机的映射，`storage`则是节点编号到其存储器的映射。  
这里到`stateMachine`是一个接口，`Raft`和`blackHole`都实现了这一个接口。我的理解是：

- `Raft`是一个活的状态机，它能够处理处理自身事物，还能够处理网络中其它节点发送过来的请求。当网络中一个 candidate 向其他节点征求选票到时候，`Raft`不是同意就是拒绝。
- `blackHole`是一个死的状态机，可以看作`Raft`宕机后的产物——它不会对任何事务作出反应。`blackHole`失去了反馈选票的能力，其他节点向`blackHole`征求选票时，`blackHole`无反应。

#### 各节点之间通信的方式
`Raft`只需要负责将需要发送到消息存入`msgs`中，并向`network`提供`Raft.Step`方法用于本`Raft`处理消息事务。`network.send`函数会先将一些消息发送到网络中某个结点之后，读取该节点产生的新信息，并根据收件人将这些信息调度运营于整个网络之中，再读取收件人产生的新信息……直到目标对象不再产生新的信息为止。

#### 项目的注意事项
同时，在调错的过程中，我还发现了一些注意事项：

- 当一个 Raft 收到高于自己 Term 的 rpc 请求时，立即变成 Follower，但需要注意此时它的 Leader 为 None，即**无领导状态**。
- 关于选举超时时间的随机化范围：如果`electionTimeOut`作为基准值是`et`个滴答，那么实际的实际值的初始化取值范围是`[et+1, 2*et)`。
- 关于候选人发送投票请求的时机：在某个节点成为候选人之后，它应该以“新一任候选人”的身份来发送`MessageType_MsgRequestVote`的请求投票的消息。但是这不应该写在`Raft.becomeCandidate()`函数里面，而应该在调用函数之后额外编写一段代码来发送消息。（来自`TestCandidateElectionTimeoutRandomized2AA`的教训）
- 关于 50% 是不是 majority：一直在想获取了 50% 的选票算不算 majority，一开始我认为是，直到测试集`TestLeaderElection2AA`告诉我不是。（其实这个可以推知时显然的，当时没转过弯来）
- 如果一个 Candidate 收到**大于** 50% 的选票，即选举成功，可成为 Leader;如果收到**大于等于** 50% 的选票，立刻选举失败并退回 Follower 的位置。

### Project 2ab

这一部分的主要内容是日志管理，也主要完成`raft.go`的内容。日志主要涉及`log.go`和`storage.go`文件。
#### Log 结构体
结构体的定义如下：
```go
type RaftLog struct {
	storage Storage
	committed uint64
	applied uint64
	stabled uint64
	entries []pb.Entry
	// (Used in 2C)
	pendingSnapshot *pb.Snapshot
	// 记录每一个 peer 收到的最大 Log 的 index 值
	peerLast map[uint64]uint64
	// 记录每个 Log index 值对应的 peer 数
	indexCount map[uint64]uint64
}
```
其中各个部分：
- `storage`：一个非易失存储，所有**已稳定**的日志条目都应该存入到其中。
- `entries`：作为所有未压缩的日志条目的集合，`storage`是它的子集。其中 storage 的存储必须与 stabled 同步。
	```
	|---stabled---|<------unstable----->|
	|<--storage-->|
	|<-------------entries------------->|
	```
- `peerLast`：记录每个 Raft 结点的 LastIndex 值。领导人根据这张 map，可以清楚地看到所有跟随者收取日志的情况，并根据这个情况确定自己的`RaftLog.committed`值。
- `indexCount`： 记录每个 LastIndex 值对应的节点个数。某个节点的 LastIndex 发生变化时，`indexCount`以 $O(1)$ 的复杂度维护。领导人遍历这个 map 决定提交的日志编号。

#### 日志初始化
在一个集群刚创建的时候，需要创建各个 Raft。而每个 Raft 的 RaftLog会由`NewLog`方法根据给定的 Storage 产生，这一过程中需要将 Storage 中所有的日志项加入 `RaftLog.entries`，并且根据非易失存储 Storage 最后的一条日志确定 `stabled` 的值。

#### HardState 参与 Raft 的初始化
Raft 初始化时用到的 Storage 中有一个 HardState 项，这一项存储了 Raft 初始化（或者重启）所需要的一些参数。当一个 Raft 宕机之后，这个 HardState 就能够及时存储宕机前的任期和投票等有效信息，并在 Raft 重启时派上用场。

- Raft 初始化时 Term 未必是 0,要看 HardState 的 Term。
- RaftLog 初始化时也请看 HardState 的 Commit。

#### 注意事项
- 领导人一旦发现自己日志的 committed 推进了，立即向其他跟随者发送 append rpc 请求，要求它们也更新自己的 committed。
- 领导人一般不会回应跟随着发来的心跳回复（MsgHeartbeatResponse），但若发现跟随者的日志更旧，则会作出回应。
- 若 Follower 和 Candidate 的日志完全一样新，则应该接受 Candidate 的投票请求。
- 我在 RaftLog 中新增了各个节点的 LastIndex 等数据。在更新这些数据的同时，需要更新对应 Raft 的 Prs，将 Match 和 Next 改为预期值。
