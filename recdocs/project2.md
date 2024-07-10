# Project2 RaftKV

实验二是要根据 raft 算法建成一个集群数据库。raft 算法的思路非常清晰，加之动画演示，让我对算法的原理了解更加深刻。和 project1 一样，也从测试集入手分析项目各部分的功能和目的，并且逐步完善。

## Project 2a
### Project 2aa
这一部分是 raft 算法基本原理的 go 实现。项目本身提供了一些结构体，这里给出我的理解。  
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

- 关于选举超时时间的随机化范围：如果`electionTimeOut`作为基准值是`et`个滴答，那么实际的实际值的初始化取值范围是`[et+1, 2*et)`。
- 关于候选人发送投票请求的时机：在某个节点成为候选人之后，它应该以“新一任候选人”的身份来发送`MessageType_MsgRequestVote`的请求投票的消息。但是这不应该写在`Raft.becomeCandidate()`函数里面，而应该在调用函数之后额外编写一段代码来发送消息。
  > 来自`TestCandidateElectionTimeoutRandomized2AA`的教训。
- 关于 50% 是不是 majority：一直在想获取了 50% 的选票算不算 majority，一开始我认为是，直到测试集`TestLeaderElection2AA`告诉我不是。