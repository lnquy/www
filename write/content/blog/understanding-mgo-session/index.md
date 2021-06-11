---
title: "Understanding Mgo Session"
description: "How Go's mgo session should be handled. Read section 1, 2, 4 for general understanding and best practices. Other sections for deep dive."
lead: "How Go's mgo session should be handled. Read section 1, 2, 4 for general understanding and best practices. Other sections for deep dive."
date: 2021-06-08T21:27:10+07:00
lastmod: 2021-06-08T21:27:10+07:00
toc: true
draft: false
weight: 50
images: ["understanding-mgo-session.jpg"]
contributors: ['quy-le']
---

### 1. Session usage

In Go's [mgo](https://github.com/globalsign/mgo), to connect and send queries to MongoDB, we have to establish a session first. 
Each mgo session can hold up to 2 sockets (connections) to the MongoDB node, one for primary node and one for secondary node.
For simplicity, let's assume we're using `Strong/Primary` consistency mode, so each session only hold at most 1 socket to the primary node, or 1 session = 1 socket.

There're 3 ways of using session when sending queries to the MongoDB: Use the same fixed session every time [1], copy the original session every time [2] and clone the original session every time [3] as figure 1.

<div class="text-center">
{{< img src="img/code.jpg" alt="Code" caption="<em>Figure 1: Three ways to use mgo session</em>" class="border-0" >}}
</div>

### 2. What is the differences between 3 approaches?  
#### 2.1. TL;DR

Let's say we have a REST API that calls to MongoDB to read some data then returns the result back in response.

1. Fixed session: Same socket will be used (reused) for all queries, no matter how many API requests we had received. 
   Because we have only 1 session, and the session is never duplicated (via `Copy()` or `Clone()`) so there's only 1 socket to the MongoDB primary node. All queries will be sent and result received via this single socket.
   That means if we have a lot of concurrent queries (e.g.: when many users calling the API at the same time) this can lead to bad throughput (the number of request/second is low) and bad latency because concurrent queries have to wait for each others to acquire the only available socket (bottleneck).
   *Generally, this is a bad approach and should be avoided if we have to deal with concurrent queries.*
   
2. Copy session: New session will be duplicated from the original session and for each query, the session will try to acquire a socket from the mgo pool. 
   If there's unused socket(s) in pool, then pool returns the socket back to session for reusing. Otherwise, if the pool is empty and the number of opened sockets is not reached `PoolLimit` (default 4096) yet, then mgo will automatically create a new socket (Dial to MongoDB node) and return it to the session.
   Once the `PoolLimit` is reached, session acquiring the socket must wait until there's an available socket being released back into the pool.
   We can control how long session should wait for the socket from pool by `PoolTimeout` (default 0, wait forever). If PoolTimeout is exceeded, the session will receive an `errPoolTimeout`.
   *=> This is the issue we saw sometimes when developer forgot to call `session.Close()` to release the underlying socket back to mgo pool and by default, session will wait forever to acquire a socket from the pool. That cause the query to MongoDB being blocked forever and application freeze.*
   
3. Clone session: New session will be duplicated same as [2], but instead of acquiring socket from the mgo pool every time, it will try to reuse the socket from the original session first if possible. 
   So, depending on the status of the original session's socket, the behavior of the cloned session can be same as fixed session [1] or copy session [2].

   ```go
   newSession := originalSession.Clone()
   defer newSession.Close()
   
   // 1. If originalSession.masterSocket is available (established and not dead yet) 
   //    => Reuse that socket directly for newSession (not calling to mgo pool).
   //    => Same behavior as fixed session [1], 1 socket will be reused for all queries.
   //
   // 2. If originalSession.masterSocket is unavailable (nil or already dead) 
   //    => Call to mgo pool to acquire socket and uses that socket for newSession.
   //    => Same behavior as copy session [2], each query has it own socket.
   ```

#### 2.2. Comparison

| Factor                                        | Fixed session                                                | Copy session                                                 | Clone session                                                |
| :-------------------------------------------- | ------------------------------------------------------------ | :----------------------------------------------------------- | ------------------------------------------------------------ |
| Acquire socket from original (parent) session | True                                                         | False                                                        | True (if original socket is ready)                           |
| Acquire socket from pool                      | False                                                        | True                                                         | True (if original socket is nil or dead)                     |
| Pros                                          | - Can forget completely about calling session.Close().<br />- Don't have to care about pool configs. | - Provide best throughput (while pool is not exhausted yet).<br />- Easier to use than clone. | - Can cope with both high / low load and concurrency requirement.<br />- Throughput can be on par with copy session (while pool is not exhausted yet).<br />- Most flexible, can handle combination of socket reuse and acquire new sockets from pool. |
| Cons                                          | - Performs really bad under high load or high concurrent environment.<br />- Cannot use all the possible performance from the system (socket bottleneck). | - Socket leak if forget to call session.Close()<br />- Can fill up the pool too quickly under burst load.<br />After pool filled up, throughput drop and latency increase.<br />- Need to understand the pool internal and its configs to use it best. | - Socket leak if forget to call session.Close().<br />- Hardest to use, must be careful and understand clearly what are you using cloned sessions for.<br />- Performance drop after pool filled up too.<br />- Need to understand the pool internal and its configs to use it best. |
| Good use at                                   | Low load or low concurrency<br />E.g.: batch job, short live simple read-only query, queries need to run in serial. | - High load and high concurrency.<br />- Long live requests that need it own socket (may have to wait for long processing and transport time). | - Both low/high load and concurrency environments.<br />- When want more control and flexible on how socket should be acquired/reused. |

#### 2.3. Best practices

1. Avoid fixed session for APIs (HTTP handler), as it's the high concurrent environment (each HTTP request is being served in a separated goroutine).
2. Make sure to understand and have good care on `DialInfo` config, especially pool configs if you're going to use `Copy()` and `Clone()`.
3. Use `Copy()` is good enough in most case if your `PoolLimit` is high enough (and MongoDB cluster can deal with such high connections load).
4. Use `Copy()` on long live queries (e.g.: write a big bulk to MongoDB or read a big list of records).
5. Careful while using `Clone()`, make sure you understand its behavior based on the original (parent) session's socket status.
6. Use `Clone()` when you need more control over socket acquisition behavior or better utilizing of system resource (e.g.: reduce pool size to reduce connection load for MongoDB cluster, but still can handle traffic load).
7. Consistency mode of a session should be set/changed right after dialed/acquired. 
   In case you want to change the consistency mode of a new session make sure to use `Copy()`, so new session will operate on it own socket.

### 3. Log details and explanations

<i style="color: #15AC8E">Note: Below is the logs and explanations for each session approach. You may want to take a look at the end of document to see the code and how to re-produce the test / how to read mgo library code first before coming back to this section.</i>

#### 3.1 Fixed session log details

##### 3.1.1. Run test with 1 workers queries 10000 times each, poolSize=10.

```shell
$ mgo_session --db-addrs mongo1:30001,mongo2:30002,mongo3:30003 --db-name BenchTest --db-username "" --db-password "" --worker 1 --query 10000 --db-pool-size 10 --stats
2021/06/03 15:16:08 ----- ONE FIXED SESSION -----
2021/06/03 15:16:08 connecting to MongoDB at: mongo1:30001,mongo2:30002,mongo3:30003
2021/06/03 15:16:08 main.getMgoRepository#beforePing: session=&mgo.Session{defaultdb:"BenchTest", sourcedb:"BenchTest", syncTimeout:10000000000, consistency:2, creds:[]mgo.Credential(nil), dialCred:(*mgo.Credential)(nil), safeOp:(*mgo.queryOp)(0xc000264000), mgoCluster:(*mgo.mongoCluster)(0xc0000be000), slaveSocket:(*mgo.mongoSocket)(nil), masterSocket:(*mgo.mongoSocket)(nil), m:sync.RWMutex{w:sync.Mutex{state:0, sema:0x0}, writerSem:0x0, readerSem:0x0, readerCount:0, readerWait:0}, queryConfig:mgo.query{op:mgo.queryOp{query:interface {}(nil), collection:"", serverTags:[]bson.D(nil), selector:interface {}(nil), replyFunc:(mgo.replyFunc)(nil), mode:0, skip:0, limit:0, options:mgo.queryWrapper{Query:interface {}(nil), OrderBy:interface {}(nil), Hint:interface {}(nil), Explain:false, Snapshot:false, ReadPreference:bson.D(nil), MaxScan:0, MaxTimeMS:0, Comment:"", Collation:(*mgo.Collation)(nil)}, hasOptions:false, flags:0x0, readConcern:""}, prefetch:0.25, limit:0}, bypassValidation:false, slaveOk:false, dialInfo:(*mgo.DialInfo)(0xc000058140)}
2021/06/03 15:16:08 connected to MongoDB
2021/06/03 15:16:08 main.getMgoRepository#NewMgoRepo: repo.db.Session=&mgo.Session{defaultdb:"BenchTest", sourcedb:"BenchTest", syncTimeout:10000000000, consistency:2, creds:[]mgo.Credential(nil), dialCred:(*mgo.Credential)(nil), safeOp:(*mgo.queryOp)(0xc000264000), mgoCluster:(*mgo.mongoCluster)(0xc0000be000), slaveSocket:(*mgo.mongoSocket)(nil), masterSocket:(*mgo.mongoSocket)(nil), m:sync.RWMutex{w:sync.Mutex{state:0, sema:0x0}, writerSem:0x0, readerSem:0x0, readerCount:0, readerWait:0}, queryConfig:mgo.query{op:mgo.queryOp{query:interface {}(nil), collection:"", serverTags:[]bson.D(nil), selector:interface {}(nil), replyFunc:(mgo.replyFunc)(nil), mode:0, skip:0, limit:0, options:mgo.queryWrapper{Query:interface {}(nil), OrderBy:interface {}(nil), Hint:interface {}(nil), Explain:false, Snapshot:false, ReadPreference:bson.D(nil), MaxScan:0, MaxTimeMS:0, Comment:"", Collation:(*mgo.Collation)(nil)}, hasOptions:false, flags:0x0, readConcern:""}, prefetch:0.25, limit:0}, bypassValidation:false, slaveOk:false, dialInfo:(*mgo.DialInfo)(0xc000058140)}
2021/06/03 15:16:08 wait for test...
2021/06/03 15:16:13 test started...
2021/06/03 15:16:14 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:744 ReceivedOps:744 ReceivedDocs:744 SocketsAlive:0 SocketsInUse:1 SocketRefs:2 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:16:15 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:1531 ReceivedOps:1530 ReceivedDocs:1530 SocketsAlive:0 SocketsInUse:1 SocketRefs:2 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:16:16 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:2322 ReceivedOps:2321 ReceivedDocs:2321 SocketsAlive:0 SocketsInUse:1 SocketRefs:2 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:16:17 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:3137 ReceivedOps:3136 ReceivedDocs:3136 SocketsAlive:0 SocketsInUse:1 SocketRefs:2 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:16:18 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:3946 ReceivedOps:3945 ReceivedDocs:3945 SocketsAlive:0 SocketsInUse:1 SocketRefs:2 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:16:19 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:4746 ReceivedOps:4745 ReceivedDocs:4745 SocketsAlive:0 SocketsInUse:1 SocketRefs:2 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:16:20 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:5535 ReceivedOps:5534 ReceivedDocs:5534 SocketsAlive:0 SocketsInUse:1 SocketRefs:2 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:16:21 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:6319 ReceivedOps:6318 ReceivedDocs:6318 SocketsAlive:0 SocketsInUse:1 SocketRefs:2 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:16:22 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:7099 ReceivedOps:7098 ReceivedDocs:7098 SocketsAlive:0 SocketsInUse:1 SocketRefs:2 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:16:23 mgo stats: {Clusters:0 MasterConns:1 SlaveConns:0 SentOps:7890 ReceivedOps:7889 ReceivedDocs:7889 SocketsAlive:1 SocketsInUse:1 SocketRefs:2 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:16:24 mgo stats: {Clusters:0 MasterConns:1 SlaveConns:0 SentOps:8687 ReceivedOps:8686 ReceivedDocs:8686 SocketsAlive:1 SocketsInUse:1 SocketRefs:2 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:16:25 mgo stats: {Clusters:0 MasterConns:1 SlaveConns:0 SentOps:9494 ReceivedOps:9493 ReceivedDocs:9493 SocketsAlive:1 SocketsInUse:1 SocketRefs:2 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:16:26 mgo stats final: {Clusters:0 MasterConns:1 SlaveConns:0 SentOps:10004 ReceivedOps:10004 ReceivedDocs:10004 SocketsAlive:1 SocketsInUse:1 SocketRefs:1 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:16:26 >>> FIXED(10000): 12.625001s
```

- Line#4 and #6: `sesion.masterSession` and `session.slaveSession` remains `nil` indicates this session hold no socket at the moment (because we have never send any query to MongoDB yet).

- Line #9 to #20: mgo statistics reports that we only tried to acquire the socket from pool once, and that's a socket to primary node.
  During test duration, the number of `SocketAlive` stays at 1, determines no other socket being created. And that socket always being used intensively.

- Line #21 reports the socket still in used, never being released back to pool even when test ended.
  No session acquiring from pool, so all pool time stats are 0.

  Note: SentOps/ReceivedOps/ReceivedDocs is always bigger than the total number of our queries, because mgo need to call to MongoDB for connection keep-alive (Ping) or syncing server metadata too.

##### 3.1.2 Run test with 10 workers queries 5000 times each, poolSize=4096.

```shell
$ mgo_session --db-addrs mongo1:30001,mongo2:30002,mongo3:30003 --db-name BenchTest --db-username "" --db-password "" --worker 10 --query 5000 --db-pool-size 4096 --stats
2021/06/03 15:30:19 ----- ONE FIXED SESSION -----
2021/06/03 15:30:19 connecting to MongoDB at: mongo1:30001,mongo2:30002,mongo3:30003
2021/06/03 15:30:19 main.getMgoRepository#beforePing: session=&mgo.Session{defaultdb:"BenchTest", sourcedb:"BenchTest", syncTimeout:10000000000, consistency:2, creds:[]mgo.Credential(nil), dialCred:(*mgo.Credential)(nil), safeOp:(*mgo.queryOp)(0xc0002100f0), mgoCluster:(*mgo.mongoCluster)(0xc0000be000), slaveSocket:(*mgo.mongoSocket)(nil), masterSocket:(*mgo.mongoSocket)(nil), m:sync.RWMutex{w:sync.Mutex{state:0, sema:0x0}, writerSem:0x0, readerSem:0x0, readerCount:0, readerWait:0}, queryConfig:mgo.query{op:mgo.queryOp{query:interface {}(nil), collection:"", serverTags:[]bson.D(nil), selector:interface {}(nil), replyFunc:(mgo.replyFunc)(nil), mode:0, skip:0, limit:0, options:mgo.queryWrapper{Query:interface {}(nil), OrderBy:interface {}(nil), Hint:interface {}(nil), Explain:false, Snapshot:false, ReadPreference:bson.D(nil), MaxScan:0, MaxTimeMS:0, Comment:"", Collation:(*mgo.Collation)(nil)}, hasOptions:false, flags:0x0, readConcern:""}, prefetch:0.25, limit:0}, bypassValidation:false, slaveOk:false, dialInfo:(*mgo.DialInfo)(0xc000058140)}
2021/06/03 15:30:19 connected to MongoDB
2021/06/03 15:30:19 main.getMgoRepository#NewMgoRepo: repo.db.Session=&mgo.Session{defaultdb:"BenchTest", sourcedb:"BenchTest", syncTimeout:10000000000, consistency:2, creds:[]mgo.Credential(nil), dialCred:(*mgo.Credential)(nil), safeOp:(*mgo.queryOp)(0xc0002100f0), mgoCluster:(*mgo.mongoCluster)(0xc0000be000), slaveSocket:(*mgo.mongoSocket)(nil), masterSocket:(*mgo.mongoSocket)(nil), m:sync.RWMutex{w:sync.Mutex{state:0, sema:0x0}, writerSem:0x0, readerSem:0x0, readerCount:0, readerWait:0}, queryConfig:mgo.query{op:mgo.queryOp{query:interface {}(nil), collection:"", serverTags:[]bson.D(nil), selector:interface {}(nil), replyFunc:(mgo.replyFunc)(nil), mode:0, skip:0, limit:0, options:mgo.queryWrapper{Query:interface {}(nil), OrderBy:interface {}(nil), Hint:interface {}(nil), Explain:false, Snapshot:false, ReadPreference:bson.D(nil), MaxScan:0, MaxTimeMS:0, Comment:"", Collation:(*mgo.Collation)(nil)}, hasOptions:false, flags:0x0, readConcern:""}, prefetch:0.25, limit:0}, bypassValidation:false, slaveOk:false, dialInfo:(*mgo.DialInfo)(0xc000058140)}
2021/06/03 15:30:19 wait for test...
2021/06/03 15:30:24 test started...
2021/06/03 15:30:25 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:3064 ReceivedOps:3054 ReceivedDocs:3054 SocketsAlive:0 SocketsInUse:1 SocketRefs:11 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:30:26 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:6712 ReceivedOps:6702 ReceivedDocs:6702 SocketsAlive:0 SocketsInUse:1 SocketRefs:11 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:30:27 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:10409 ReceivedOps:10400 ReceivedDocs:10400 SocketsAlive:0 SocketsInUse:1 SocketRefs:11 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:30:28 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:14088 ReceivedOps:14078 ReceivedDocs:14078 SocketsAlive:0 SocketsInUse:1 SocketRefs:11 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:30:29 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:17833 ReceivedOps:17824 ReceivedDocs:17824 SocketsAlive:0 SocketsInUse:1 SocketRefs:11 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:30:30 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:21561 ReceivedOps:21551 ReceivedDocs:21551 SocketsAlive:0 SocketsInUse:1 SocketRefs:11 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:30:31 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:25322 ReceivedOps:25312 ReceivedDocs:25312 SocketsAlive:0 SocketsInUse:1 SocketRefs:11 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:30:32 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:28951 ReceivedOps:28941 ReceivedDocs:28941 SocketsAlive:0 SocketsInUse:1 SocketRefs:11 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:30:33 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:32721 ReceivedOps:32712 ReceivedDocs:32712 SocketsAlive:0 SocketsInUse:1 SocketRefs:11 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:30:34 mgo stats: {Clusters:0 MasterConns:1 SlaveConns:0 SentOps:36474 ReceivedOps:36464 ReceivedDocs:36464 SocketsAlive:1 SocketsInUse:1 SocketRefs:11 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:30:35 mgo stats: {Clusters:0 MasterConns:1 SlaveConns:0 SentOps:40218 ReceivedOps:40208 ReceivedDocs:40208 SocketsAlive:1 SocketsInUse:1 SocketRefs:11 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:30:36 mgo stats: {Clusters:0 MasterConns:1 SlaveConns:0 SentOps:43933 ReceivedOps:43923 ReceivedDocs:43923 SocketsAlive:1 SocketsInUse:1 SocketRefs:11 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:30:37 mgo stats: {Clusters:0 MasterConns:1 SlaveConns:0 SentOps:47689 ReceivedOps:47680 ReceivedDocs:47680 SocketsAlive:1 SocketsInUse:1 SocketRefs:11 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:30:37 mgo stats final: {Clusters:0 MasterConns:1 SlaveConns:0 SentOps:50004 ReceivedOps:50004 ReceivedDocs:50004 SocketsAlive:1 SocketsInUse:1 SocketRefs:1 TimesSocketAcquired:1 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:30:37 >>> FIXED(50000): 13.6784755s
```

- Line #9 to #21: Same as previous test. Only difference now is SocketRefs is increased to 11, indicate more sessions trying to hold/acquire the socket from the pool (blocking).
- Pool size config has no effect here as it show.

#### 3.2. Copy session log details

##### 3.2.1 Run test with 10 workers queries 5000 times each, poolSize=4096.

```shell
$ mgo_session --db-addrs mongo1:30001,mongo2:30002,mongo3:30003 --db-name BenchTest --db-username "" --db-password "" --worker 10 --query 5000 --db-pool-size 4096 --stats
2021/06/03 15:44:16 ----- SESSION COPY -----
2021/06/03 15:44:16 connecting to MongoDB at: mongo1:30001,mongo2:30002,mongo3:30003
2021/06/03 15:44:16 main.getMgoRepository#beforePing: session=&mgo.Session{defaultdb:"BenchTest", sourcedb:"BenchTest", syncTimeout:10000000000, consistency:2, creds:[]mgo.Credential(nil), dialCred:(*mgo.Credential)(nil), safeOp:(*mgo.queryOp)(0xc00019e2d0), mgoCluster:(*mgo.mongoCluster)(0xc0000be000), slaveSocket:(*mgo.mongoSocket)(nil), masterSocket:(*mgo.mongoSocket)(nil), m:sync.RWMutex{w:sync.Mutex{state:0, sema:0x0}, writerSem:0x0, readerSem:0x0, readerCount:0, readerWait:0}, queryConfig:mgo.query{op:mgo.queryOp{query:interface {}(nil), collection:"", serverTags:[]bson.D(nil), selector:interface {}(nil), replyFunc:(mgo.replyFunc)(nil), mode:0, skip:0, limit:0, options:mgo.queryWrapper{Query:interface {}(nil), OrderBy:interface {}(nil), Hint:interface {}(nil), Explain:false, Snapshot:false, ReadPreference:bson.D(nil), MaxScan:0, MaxTimeMS:0, Comment:"", Collation:(*mgo.Collation)(nil)}, hasOptions:false, flags:0x0, readConcern:""}, prefetch:0.25, limit:0}, bypassValidation:false, slaveOk:false, dialInfo:(*mgo.DialInfo)(0xc000058140)}
2021/06/03 15:44:16 connected to MongoDB
2021/06/03 15:44:16 main.getMgoRepository#NewMgoRepo: repo.db.Session=&mgo.Session{defaultdb:"BenchTest", sourcedb:"BenchTest", syncTimeout:10000000000, consistency:2, creds:[]mgo.Credential(nil), dialCred:(*mgo.Credential)(nil), safeOp:(*mgo.queryOp)(0xc00019e2d0), mgoCluster:(*mgo.mongoCluster)(0xc0000be000), slaveSocket:(*mgo.mongoSocket)(nil), masterSocket:(*mgo.mongoSocket)(nil), m:sync.RWMutex{w:sync.Mutex{state:0, sema:0x0}, writerSem:0x0, readerSem:0x0, readerCount:0, readerWait:0}, queryConfig:mgo.query{op:mgo.queryOp{query:interface {}(nil), collection:"", serverTags:[]bson.D(nil), selector:interface {}(nil), replyFunc:(mgo.replyFunc)(nil), mode:0, skip:0, limit:0, options:mgo.queryWrapper{Query:interface {}(nil), OrderBy:interface {}(nil), Hint:interface {}(nil), Explain:false, Snapshot:false, ReadPreference:bson.D(nil), MaxScan:0, MaxTimeMS:0, Comment:"", Collation:(*mgo.Collation)(nil)}, hasOptions:false, flags:0x0, readConcern:""}, prefetch:0.25, limit:0}, bypassValidation:false, slaveOk:false, dialInfo:(*mgo.DialInfo)(0xc000058140)}
2021/06/03 15:44:16 wait for test...
2021/06/03 15:44:21 test started...
2021/06/03 15:44:22 mgo stats: {Clusters:0 MasterConns:9 SlaveConns:0 SentOps:3856 ReceivedOps:3846 ReceivedDocs:3846 SocketsAlive:9 SocketsInUse:10 SocketRefs:20 TimesSocketAcquired:3847 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:44:23 mgo stats: {Clusters:0 MasterConns:9 SlaveConns:0 SentOps:9130 ReceivedOps:9120 ReceivedDocs:9120 SocketsAlive:9 SocketsInUse:10 SocketRefs:20 TimesSocketAcquired:9121 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:44:24 mgo stats: {Clusters:0 MasterConns:9 SlaveConns:0 SentOps:14403 ReceivedOps:14393 ReceivedDocs:14393 SocketsAlive:9 SocketsInUse:10 SocketRefs:20 TimesSocketAcquired:14394 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:44:25 mgo stats: {Clusters:0 MasterConns:9 SlaveConns:0 SentOps:19646 ReceivedOps:19636 ReceivedDocs:19636 SocketsAlive:9 SocketsInUse:10 SocketRefs:20 TimesSocketAcquired:19637 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:44:26 mgo stats: {Clusters:0 MasterConns:9 SlaveConns:0 SentOps:24942 ReceivedOps:24932 ReceivedDocs:24932 SocketsAlive:9 SocketsInUse:10 SocketRefs:20 TimesSocketAcquired:24933 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:44:27 mgo stats: {Clusters:0 MasterConns:9 SlaveConns:0 SentOps:30242 ReceivedOps:30232 ReceivedDocs:30232 SocketsAlive:9 SocketsInUse:10 SocketRefs:20 TimesSocketAcquired:30233 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:44:28 mgo stats: {Clusters:0 MasterConns:9 SlaveConns:0 SentOps:35505 ReceivedOps:35495 ReceivedDocs:35495 SocketsAlive:9 SocketsInUse:10 SocketRefs:20 TimesSocketAcquired:35496 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:44:29 mgo stats: {Clusters:0 MasterConns:9 SlaveConns:0 SentOps:40740 ReceivedOps:40730 ReceivedDocs:40730 SocketsAlive:9 SocketsInUse:10 SocketRefs:20 TimesSocketAcquired:40731 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:44:30 mgo stats: {Clusters:0 MasterConns:9 SlaveConns:0 SentOps:45914 ReceivedOps:45904 ReceivedDocs:45904 SocketsAlive:9 SocketsInUse:10 SocketRefs:20 TimesSocketAcquired:45905 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:44:31 mgo stats final: {Clusters:0 MasterConns:9 SlaveConns:0 SentOps:50009 ReceivedOps:50009 ReceivedDocs:50009 SocketsAlive:9 SocketsInUse:0 SocketRefs:0 TimesSocketAcquired:50000 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:44:31 >>> COPY(50000): 9.9733231s
```

- Line#4 and #6: `sesion.masterSession` and `session.slaveSession` remains `nil` indicates this session hold no socket at the moment (because we have never send any query to MongoDB yet).

- Line #9 to #18 reports we tried to acquire socket from pool exactly 50000 times, there're 9 sockets opened to primary node and all 9 (10?) are being used concurrently. 

  No timeout or blocking time waiting for socket acquire as the number of opened socket is is much less than PoolLimit (9 << 4096).
  Same amount of queries at previous test on fixed session, but it finished faster (10s vs 13.7s).

##### 3.2.2 Run test with 100 workers queries 3000 times each, poolSize=50 => Reach PoolLimit

```shell
$ mgo_session --db-addrs mongo1:30001,mongo2:30002,mongo3:30003 --db-name BenchTest --db-username "" --db-password "" --worker 100 --query 3000 --db-pool-size 50 --stats
2021/06/03 15:52:59 ----- SESSION COPY -----
2021/06/03 15:52:59 connecting to MongoDB at: mongo1:30001,mongo2:30002,mongo3:30003
2021/06/03 15:52:59 main.getMgoRepository#beforePing: session=&mgo.Session{defaultdb:"BenchTest", sourcedb:"BenchTest", syncTimeout:10000000000, consistency:2, creds:[]mgo.Credential(nil), dialCred:(*mgo.Credential)(nil), safeOp:(*mgo.queryOp)(0xc00025a000), mgoCluster:(*mgo.mongoCluster)(0xc0000be000), slaveSocket:(*mgo.mongoSocket)(nil), masterSocket:(*mgo.mongoSocket)(nil), m:sync.RWMutex{w:sync.Mutex{state:0, sema:0x0}, writerSem:0x0, readerSem:0x0, readerCount:0, readerWait:0}, queryConfig:mgo.query{op:mgo.queryOp{query:interface {}(nil), collection:"", serverTags:[]bson.D(nil), selector:interface {}(nil), replyFunc:(mgo.replyFunc)(nil), mode:0, skip:0, limit:0, options:mgo.queryWrapper{Query:interface {}(nil), OrderBy:interface {}(nil), Hint:interface {}(nil), Explain:false, Snapshot:false, ReadPreference:bson.D(nil), MaxScan:0, MaxTimeMS:0, Comment:"", Collation:(*mgo.Collation)(nil)}, hasOptions:false, flags:0x0, readConcern:""}, prefetch:0.25, limit:0}, bypassValidation:false, slaveOk:false, dialInfo:(*mgo.DialInfo)(0xc000058140)}
2021/06/03 15:52:59 connected to MongoDB
2021/06/03 15:52:59 main.getMgoRepository#NewMgoRepo: repo.db.Session=&mgo.Session{defaultdb:"BenchTest", sourcedb:"BenchTest", syncTimeout:10000000000, consistency:2, creds:[]mgo.Credential(nil), dialCred:(*mgo.Credential)(nil), safeOp:(*mgo.queryOp)(0xc00025a000), mgoCluster:(*mgo.mongoCluster)(0xc0000be000), slaveSocket:(*mgo.mongoSocket)(nil), masterSocket:(*mgo.mongoSocket)(nil), m:sync.RWMutex{w:sync.Mutex{state:0, sema:0x0}, writerSem:0x0, readerSem:0x0, readerCount:0, readerWait:0}, queryConfig:mgo.query{op:mgo.queryOp{query:interface {}(nil), collection:"", serverTags:[]bson.D(nil), selector:interface {}(nil), replyFunc:(mgo.replyFunc)(nil), mode:0, skip:0, limit:0, options:mgo.queryWrapper{Query:interface {}(nil), OrderBy:interface {}(nil), Hint:interface {}(nil), Explain:false, Snapshot:false, ReadPreference:bson.D(nil), MaxScan:0, MaxTimeMS:0, Comment:"", Collation:(*mgo.Collation)(nil)}, hasOptions:false, flags:0x0, readConcern:""}, prefetch:0.25, limit:0}, bypassValidation:false, slaveOk:false, dialInfo:(*mgo.DialInfo)(0xc000058140)}
2021/06/03 15:52:59 wait for test...
2021/06/03 15:53:04 test started...
2021/06/03 15:53:05 mgo stats: {Clusters:0 MasterConns:18 SlaveConns:0 SentOps:4786 ReceivedOps:4767 ReceivedDocs:4767 SocketsAlive:18 SocketsInUse:19 SocketRefs:38 TimesSocketAcquired:4768 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:53:06 mgo stats: {Clusters:0 MasterConns:38 SlaveConns:0 SentOps:15291 ReceivedOps:15252 ReceivedDocs:15252 SocketsAlive:38 SocketsInUse:39 SocketRefs:78 TimesSocketAcquired:15253 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 15:53:07 mgo stats: {Clusters:0 MasterConns:49 SlaveConns:0 SentOps:28331 ReceivedOps:28281 ReceivedDocs:28281 SocketsAlive:49 SocketsInUse:50 SocketRefs:100 TimesSocketAcquired:28282 TimesWaitedForPool:943 TotalPoolWaitTime:1.7759267s PoolTimeouts:0}
2021/06/03 15:53:08 mgo stats: {Clusters:0 MasterConns:49 SlaveConns:0 SentOps:41713 ReceivedOps:41667 ReceivedDocs:41667 SocketsAlive:49 SocketsInUse:48 SocketRefs:96 TimesSocketAcquired:41664 TimesWaitedForPool:4929 TotalPoolWaitTime:19.8262276s PoolTimeouts:0}
2021/06/03 15:53:09 mgo stats: {Clusters:0 MasterConns:49 SlaveConns:0 SentOps:55152 ReceivedOps:55102 ReceivedDocs:55102 SocketsAlive:49 SocketsInUse:50 SocketRefs:100 TimesSocketAcquired:55103 TimesWaitedForPool:10245 TotalPoolWaitTime:57.2233521s PoolTimeouts:0}
2021/06/03 15:53:10 mgo stats: {Clusters:0 MasterConns:49 SlaveConns:0 SentOps:68470 ReceivedOps:68420 ReceivedDocs:68420 SocketsAlive:49 SocketsInUse:50 SocketRefs:100 TimesSocketAcquired:68421 TimesWaitedForPool:15794 TotalPoolWaitTime:1m46.5722161s PoolTimeouts:0}
2021/06/03 15:53:11 mgo stats: {Clusters:0 MasterConns:49 SlaveConns:0 SentOps:81743 ReceivedOps:81693 ReceivedDocs:81693 SocketsAlive:49 SocketsInUse:50 SocketRefs:100 TimesSocketAcquired:81694 TimesWaitedForPool:21393 TotalPoolWaitTime:2m36.2709841s PoolTimeouts:0}
2021/06/03 15:53:12 mgo stats: {Clusters:0 MasterConns:49 SlaveConns:0 SentOps:94928 ReceivedOps:94878 ReceivedDocs:94878 SocketsAlive:49 SocketsInUse:50 SocketRefs:100 TimesSocketAcquired:94879 TimesWaitedForPool:26864 TotalPoolWaitTime:3m25.7365406s PoolTimeouts:0}
2021/06/03 15:53:13 mgo stats: {Clusters:0 MasterConns:49 SlaveConns:0 SentOps:108270 ReceivedOps:108249 ReceivedDocs:108249 SocketsAlive:49 SocketsInUse:38 SocketRefs:76 TimesSocketAcquired:108234 TimesWaitedForPool:32362 TotalPoolWaitTime:4m15.2711986s PoolTimeouts:0}
2021/06/03 15:53:14 mgo stats: {Clusters:0 MasterConns:49 SlaveConns:0 SentOps:121679 ReceivedOps:121629 ReceivedDocs:121629 SocketsAlive:49 SocketsInUse:50 SocketRefs:100 TimesSocketAcquired:121628 TimesWaitedForPool:37789 TotalPoolWaitTime:5m4.5723571s PoolTimeouts:0}
2021/06/03 15:53:15 mgo stats: {Clusters:0 MasterConns:49 SlaveConns:0 SentOps:134946 ReceivedOps:134896 ReceivedDocs:134896 SocketsAlive:49 SocketsInUse:50 SocketRefs:100 TimesSocketAcquired:134895 TimesWaitedForPool:43297 TotalPoolWaitTime:5m54.1085701s PoolTimeouts:0}
2021/06/03 15:53:16 mgo stats: {Clusters:0 MasterConns:49 SlaveConns:0 SentOps:147917 ReceivedOps:147867 ReceivedDocs:147867 SocketsAlive:49 SocketsInUse:50 SocketRefs:100 TimesSocketAcquired:147866 TimesWaitedForPool:48591 TotalPoolWaitTime:6m43.6398708s PoolTimeouts:0}
2021/06/03 15:53:17 mgo stats: {Clusters:0 MasterConns:49 SlaveConns:0 SentOps:161066 ReceivedOps:161018 ReceivedDocs:161018 SocketsAlive:49 SocketsInUse:50 SocketRefs:100 TimesSocketAcquired:161017 TimesWaitedForPool:53950 TotalPoolWaitTime:7m33.316644s PoolTimeouts:0}
2021/06/03 15:53:18 mgo stats: {Clusters:0 MasterConns:49 SlaveConns:0 SentOps:174385 ReceivedOps:174339 ReceivedDocs:174339 SocketsAlive:49 SocketsInUse:49 SocketRefs:98 TimesSocketAcquired:174334 TimesWaitedForPool:59299 TotalPoolWaitTime:8m22.6848505s PoolTimeouts:0}
2021/06/03 15:53:19 mgo stats: {Clusters:0 MasterConns:49 SlaveConns:0 SentOps:187625 ReceivedOps:187575 ReceivedDocs:187575 SocketsAlive:49 SocketsInUse:50 SocketRefs:100 TimesSocketAcquired:187574 TimesWaitedForPool:65009 TotalPoolWaitTime:9m12.1500819s PoolTimeouts:0}
2021/06/03 15:53:20 mgo stats: {Clusters:0 MasterConns:49 SlaveConns:0 SentOps:200926 ReceivedOps:200877 ReceivedDocs:200877 SocketsAlive:49 SocketsInUse:50 SocketRefs:100 TimesSocketAcquired:200876 TimesWaitedForPool:70569 TotalPoolWaitTime:10m1.7556688s PoolTimeouts:0}
2021/06/03 15:53:21 mgo stats: {Clusters:0 MasterConns:49 SlaveConns:0 SentOps:214250 ReceivedOps:214200 ReceivedDocs:214200 SocketsAlive:49 SocketsInUse:50 SocketRefs:100 TimesSocketAcquired:214199 TimesWaitedForPool:76178 TotalPoolWaitTime:10m50.227758s PoolTimeouts:0}
2021/06/03 15:53:22 mgo stats: {Clusters:0 MasterConns:49 SlaveConns:0 SentOps:227623 ReceivedOps:227573 ReceivedDocs:227573 SocketsAlive:49 SocketsInUse:50 SocketRefs:100 TimesSocketAcquired:227572 TimesWaitedForPool:81414 TotalPoolWaitTime:11m35.459438s PoolTimeouts:0}
2021/06/03 15:53:23 mgo stats: {Clusters:0 MasterConns:49 SlaveConns:0 SentOps:241009 ReceivedOps:240959 ReceivedDocs:240959 SocketsAlive:49 SocketsInUse:50 SocketRefs:100 TimesSocketAcquired:240958 TimesWaitedForPool:86684 TotalPoolWaitTime:12m13.5718605s PoolTimeouts:0}
2021/06/03 15:53:24 mgo stats: {Clusters:0 MasterConns:49 SlaveConns:0 SentOps:254505 ReceivedOps:254455 ReceivedDocs:254455 SocketsAlive:49 SocketsInUse:50 SocketRefs:100 TimesSocketAcquired:254454 TimesWaitedForPool:91881 TotalPoolWaitTime:12m46.2093818s PoolTimeouts:0}
2021/06/03 15:53:25 mgo stats: {Clusters:0 MasterConns:49 SlaveConns:0 SentOps:268057 ReceivedOps:268007 ReceivedDocs:268007 SocketsAlive:49 SocketsInUse:50 SocketRefs:100 TimesSocketAcquired:268006 TimesWaitedForPool:96460 TotalPoolWaitTime:13m10.9517656s PoolTimeouts:0}
2021/06/03 15:53:26 mgo stats: {Clusters:0 MasterConns:49 SlaveConns:0 SentOps:281417 ReceivedOps:281367 ReceivedDocs:281367 SocketsAlive:49 SocketsInUse:50 SocketRefs:100 TimesSocketAcquired:281366 TimesWaitedForPool:100555 TotalPoolWaitTime:13m25.7847828s PoolTimeouts:0}
2021/06/03 15:53:27 mgo stats: {Clusters:0 MasterConns:49 SlaveConns:0 SentOps:294462 ReceivedOps:294429 ReceivedDocs:294429 SocketsAlive:49 SocketsInUse:33 SocketRefs:66 TimesSocketAcquired:294411 TimesWaitedForPool:101547 TotalPoolWaitTime:13m27.8194479s PoolTimeouts:0}
2021/06/03 15:53:27 mgo stats final: {Clusters:0 MasterConns:49 SlaveConns:0 SentOps:300051 ReceivedOps:300051 ReceivedDocs:300051 SocketsAlive:49 SocketsInUse:0 SocketRefs:0 TimesSocketAcquired:300000 TimesWaitedForPool:101547 TotalPoolWaitTime:13m27.8194479s PoolTimeouts:0}
2021/06/03 15:53:27 >>> COPY(300000): 23.6601136s
```

- Line #9 to #32 reports we opened 49 sockets to primary node and use them all during test. 
  Stats of pool time indicates we have to wait to acquire sockets from pool many times => Pool exhausted.

#### 3.3. Clone session log details

##### 3.3.1. Run test with 10 workers queries 5000 times each, poolSize=4096, initialize original session after dial => masterSocket is ready => Same behavior and performance as fixed session [1]

```shell
$ mgo_session --db-addrs mongo1:30001,mongo2:30002,mongo3:30003 --db-name BenchTest --db-username "" --db-password "" --worker 10 --query 5000 --db-pool-size 4096 --stats --session-first-ping
2021/06/03 16:01:22 ----- SESSION CLONE -----
2021/06/03 16:01:22 connecting to MongoDB at: mongo1:30001,mongo2:30002,mongo3:30003
2021/06/03 16:01:22 main.getMgoRepository#beforePing: session=&mgo.Session{defaultdb:"BenchTest", sourcedb:"BenchTest", syncTimeout:10000000000, consistency:2, creds:[]mgo.Credential(nil), dialCred:(*mgo.Credential)(nil), safeOp:(*mgo.queryOp)(0xc00024c000), mgoCluster:(*mgo.mongoCluster)(0xc0000bc000), slaveSocket:(*mgo.mongoSocket)(nil), masterSocket:(*mgo.mongoSocket)(nil), m:sync.RWMutex{w:sync.Mutex{state:0, sema:0x0}, writerSem:0x0, readerSem:0x0, readerCount:0, readerWait:0}, queryConfig:mgo.query{op:mgo.queryOp{query:interface {}(nil), collection:"", serverTags:[]bson.D(nil), selector:interface {}(nil), replyFunc:(mgo.replyFunc)(nil), mode:0, skip:0, limit:0, options:mgo.queryWrapper{Query:interface {}(nil), OrderBy:interface {}(nil), Hint:interface {}(nil), Explain:false, Snapshot:false, ReadPreference:bson.D(nil), MaxScan:0, MaxTimeMS:0, Comment:"", Collation:(*mgo.Collation)(nil)}, hasOptions:false, flags:0x0, readConcern:""}, prefetch:0.25, limit:0}, bypassValidation:false, slaveOk:false, dialInfo:(*mgo.DialInfo)(0xc000058140)}
2021/06/03 16:01:22 *session-first-ping flag enabled, the original session should have masterSocket ready after calling Ping()
2021/06/03 16:01:22 main.getMgoRepository#afterPing: session=&mgo.Session{defaultdb:"BenchTest", sourcedb:"BenchTest", syncTimeout:10000000000, consistency:2, creds:[]mgo.Credential(nil), dialCred:(*mgo.Credential)(nil), safeOp:(*mgo.queryOp)(0xc00024c000), mgoCluster:(*mgo.mongoCluster)(0xc0000bc000), slaveSocket:(*mgo.mongoSocket)(nil), masterSocket:(*mgo.mongoSocket)(0xc00018e000), m:sync.RWMutex{w:sync.Mutex{state:0, sema:0x0}, writerSem:0x0, readerSem:0x0, readerCount:0, readerWait:0}, queryConfig:mgo.query{op:mgo.queryOp{query:interface {}(nil), collection:"", serverTags:[]bson.D(nil), selector:interface {}(nil), replyFunc:(mgo.replyFunc)(nil), mode:0, skip:0, limit:0, options:mgo.queryWrapper{Query:interface {}(nil), OrderBy:interface {}(nil), Hint:interface {}(nil), Explain:false, Snapshot:false, ReadPreference:bson.D(nil), MaxScan:0, MaxTimeMS:0, Comment:"", Collation:(*mgo.Collation)(nil)}, hasOptions:false, flags:0x0, readConcern:""}, prefetch:0.25, limit:0}, bypassValidation:false, slaveOk:false, dialInfo:(*mgo.DialInfo)(0xc000058140)}
2021/06/03 16:01:22 connected to MongoDB
2021/06/03 16:01:22 main.getMgoRepository#NewMgoRepo: repo.db.Session=&mgo.Session{defaultdb:"BenchTest", sourcedb:"BenchTest", syncTimeout:10000000000, consistency:2, creds:[]mgo.Credential(nil), dialCred:(*mgo.Credential)(nil), safeOp:(*mgo.queryOp)(0xc00024c000), mgoCluster:(*mgo.mongoCluster)(0xc0000bc000), slaveSocket:(*mgo.mongoSocket)(nil), masterSocket:(*mgo.mongoSocket)(0xc00018e000), m:sync.RWMutex{w:sync.Mutex{state:0, sema:0x0}, writerSem:0x0, readerSem:0x0, readerCount:0, readerWait:0}, queryConfig:mgo.query{op:mgo.queryOp{query:interface {}(nil), collection:"", serverTags:[]bson.D(nil), selector:interface {}(nil), replyFunc:(mgo.replyFunc)(nil), mode:0, skip:0, limit:0, options:mgo.queryWrapper{Query:interface {}(nil), OrderBy:interface {}(nil), Hint:interface {}(nil), Explain:false, Snapshot:false, ReadPreference:bson.D(nil), MaxScan:0, MaxTimeMS:0, Comment:"", Collation:(*mgo.Collation)(nil)}, hasOptions:false, flags:0x0, readConcern:""}, prefetch:0.25, limit:0}, bypassValidation:false, slaveOk:false, dialInfo:(*mgo.DialInfo)(0xc000058140)}
2021/06/03 16:01:22 wait for test...
2021/06/03 16:01:27 test started...
2021/06/03 16:01:28 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:2992 ReceivedOps:2982 ReceivedDocs:2982 SocketsAlive:0 SocketsInUse:0 SocketRefs:20 TimesSocketAcquired:0 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 16:01:29 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:6690 ReceivedOps:6681 ReceivedDocs:6681 SocketsAlive:0 SocketsInUse:0 SocketRefs:20 TimesSocketAcquired:0 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 16:01:30 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:10284 ReceivedOps:10274 ReceivedDocs:10274 SocketsAlive:0 SocketsInUse:0 SocketRefs:20 TimesSocketAcquired:0 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 16:01:31 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:13830 ReceivedOps:13821 ReceivedDocs:13821 SocketsAlive:0 SocketsInUse:0 SocketRefs:20 TimesSocketAcquired:0 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 16:01:32 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:17400 ReceivedOps:17390 ReceivedDocs:17390 SocketsAlive:0 SocketsInUse:0 SocketRefs:20 TimesSocketAcquired:0 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 16:01:33 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:20967 ReceivedOps:20957 ReceivedDocs:20957 SocketsAlive:0 SocketsInUse:0 SocketRefs:20 TimesSocketAcquired:0 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 16:01:34 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:24609 ReceivedOps:24600 ReceivedDocs:24600 SocketsAlive:0 SocketsInUse:0 SocketRefs:20 TimesSocketAcquired:0 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 16:01:35 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:28315 ReceivedOps:28305 ReceivedDocs:28305 SocketsAlive:0 SocketsInUse:0 SocketRefs:20 TimesSocketAcquired:0 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 16:01:36 mgo stats: {Clusters:0 MasterConns:0 SlaveConns:0 SentOps:31911 ReceivedOps:31901 ReceivedDocs:31901 SocketsAlive:0 SocketsInUse:0 SocketRefs:20 TimesSocketAcquired:0 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 16:01:37 mgo stats: {Clusters:0 MasterConns:1 SlaveConns:0 SentOps:35687 ReceivedOps:35679 ReceivedDocs:35679 SocketsAlive:1 SocketsInUse:0 SocketRefs:20 TimesSocketAcquired:0 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 16:01:38 mgo stats: {Clusters:0 MasterConns:1 SlaveConns:0 SentOps:39243 ReceivedOps:39234 ReceivedDocs:39234 SocketsAlive:1 SocketsInUse:0 SocketRefs:20 TimesSocketAcquired:0 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 16:01:39 mgo stats: {Clusters:0 MasterConns:1 SlaveConns:0 SentOps:42913 ReceivedOps:42904 ReceivedDocs:42904 SocketsAlive:1 SocketsInUse:0 SocketRefs:20 TimesSocketAcquired:0 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 16:01:40 mgo stats: {Clusters:0 MasterConns:1 SlaveConns:0 SentOps:46623 ReceivedOps:46613 ReceivedDocs:46613 SocketsAlive:1 SocketsInUse:0 SocketRefs:20 TimesSocketAcquired:0 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 16:01:41 mgo stats final: {Clusters:0 MasterConns:1 SlaveConns:0 SentOps:50004 ReceivedOps:50004 ReceivedDocs:50004 SocketsAlive:1 SocketsInUse:0 SocketRefs:0 TimesSocketAcquired:0 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 16:01:41 >>> CLONE(50000): 13.9907703s
```

- Line #4 shows masterSocket and slaveSocket is nil before calling Ping().

- Line #6 and #8 shows masterSocket is ready after calling Ping() `masterSocket:(*mgo.mongoSocket)(0xc00018e000)`.

  The original session has it masterSocket ready, that means next call to Clone() will reuse that masterSocket instead of acquiring from pool => Same behavior with fixed session [1].

- Line #11 to #24 reports same behavior as fixed session, only 1 socket is being used, and no call to pool.

##### 3.3.2 Run test with 10 workers queries 5000 times each, poolSize=4096, no session initialize after dial => masterSocket is nil => Same behavior and performance as copy session [2]

```shell
$ mgo_session --db-addrs mongo1:30001,mongo2:30002,mongo3:30003 --db-name BenchTest --db-username "" --db-password "" --worker 10 --query 5000 --db-pool-size 4096 --stats
2021/06/03 16:08:20 ----- SESSION CLONE -----
2021/06/03 16:08:20 connecting to MongoDB at: mongo1:30001,mongo2:30002,mongo3:30003
2021/06/03 16:08:20 main.getMgoRepository#beforePing: session=&mgo.Session{defaultdb:"BenchTest", sourcedb:"BenchTest", syncTimeout:10000000000, consistency:2, creds:[]mgo.Credential(nil), dialCred:(*mgo.Credential)(nil), safeOp:(*mgo.queryOp)(0xc0000be2d0), mgoCluster:(*mgo.mongoCluster)(0xc0000be000), slaveSocket:(*mgo.mongoSocket)(nil), masterSocket:(*mgo.mongoSocket)(nil), m:sync.RWMutex{w:sync.Mutex{state:0, sema:0x0}, writerSem:0x0, readerSem:0x0, readerCount:0, readerWait:0}, queryConfig:mgo.query{op:mgo.queryOp{query:interface {}(nil), collection:"", serverTags:[]bson.D(nil), selector:interface {}(nil), replyFunc:(mgo.replyFunc)(nil), mode:0, skip:0, limit:0, options:mgo.queryWrapper{Query:interface {}(nil), OrderBy:interface {}(nil), Hint:interface {}(nil), Explain:false, Snapshot:false, ReadPreference:bson.D(nil), MaxScan:0, MaxTimeMS:0, Comment:"", Collation:(*mgo.Collation)(nil)}, hasOptions:false, flags:0x0, readConcern:""}, prefetch:0.25, limit:0}, bypassValidation:false, slaveOk:false, dialInfo:(*mgo.DialInfo)(0xc000058140)}
2021/06/03 16:08:20 connected to MongoDB
2021/06/03 16:08:20 main.getMgoRepository#NewMgoRepo: repo.db.Session=&mgo.Session{defaultdb:"BenchTest", sourcedb:"BenchTest", syncTimeout:10000000000, consistency:2, creds:[]mgo.Credential(nil), dialCred:(*mgo.Credential)(nil), safeOp:(*mgo.queryOp)(0xc0000be2d0), mgoCluster:(*mgo.mongoCluster)(0xc0000be000), slaveSocket:(*mgo.mongoSocket)(nil), masterSocket:(*mgo.mongoSocket)(nil), m:sync.RWMutex{w:sync.Mutex{state:0, sema:0x0}, writerSem:0x0, readerSem:0x0, readerCount:0, readerWait:0}, queryConfig:mgo.query{op:mgo.queryOp{query:interface {}(nil), collection:"", serverTags:[]bson.D(nil), selector:interface {}(nil), replyFunc:(mgo.replyFunc)(nil), mode:0, skip:0, limit:0, options:mgo.queryWrapper{Query:interface {}(nil), OrderBy:interface {}(nil), Hint:interface {}(nil), Explain:false, Snapshot:false, ReadPreference:bson.D(nil), MaxScan:0, MaxTimeMS:0, Comment:"", Collation:(*mgo.Collation)(nil)}, hasOptions:false, flags:0x0, readConcern:""}, prefetch:0.25, limit:0}, bypassValidation:false, slaveOk:false, dialInfo:(*mgo.DialInfo)(0xc000058140)}
2021/06/03 16:08:20 wait for test...
2021/06/03 16:08:25 test started...
2021/06/03 16:08:26 mgo stats: {Clusters:0 MasterConns:9 SlaveConns:0 SentOps:3848 ReceivedOps:3838 ReceivedDocs:3838 SocketsAlive:9 SocketsInUse:10 SocketRefs:20 TimesSocketAcquired:3839 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 16:08:27 mgo stats: {Clusters:0 MasterConns:9 SlaveConns:0 SentOps:9103 ReceivedOps:9093 ReceivedDocs:9093 SocketsAlive:9 SocketsInUse:10 SocketRefs:20 TimesSocketAcquired:9094 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 16:08:28 mgo stats: {Clusters:0 MasterConns:9 SlaveConns:0 SentOps:14427 ReceivedOps:14417 ReceivedDocs:14417 SocketsAlive:9 SocketsInUse:10 SocketRefs:20 TimesSocketAcquired:14418 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 16:08:29 mgo stats: {Clusters:0 MasterConns:9 SlaveConns:0 SentOps:19687 ReceivedOps:19677 ReceivedDocs:19677 SocketsAlive:9 SocketsInUse:10 SocketRefs:20 TimesSocketAcquired:19678 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 16:08:30 mgo stats: {Clusters:0 MasterConns:9 SlaveConns:0 SentOps:25007 ReceivedOps:25000 ReceivedDocs:25000 SocketsAlive:9 SocketsInUse:10 SocketRefs:20 TimesSocketAcquired:24998 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 16:08:31 mgo stats: {Clusters:0 MasterConns:9 SlaveConns:0 SentOps:30281 ReceivedOps:30271 ReceivedDocs:30271 SocketsAlive:9 SocketsInUse:10 SocketRefs:20 TimesSocketAcquired:30272 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 16:08:32 mgo stats: {Clusters:0 MasterConns:9 SlaveConns:0 SentOps:35563 ReceivedOps:35553 ReceivedDocs:35553 SocketsAlive:9 SocketsInUse:10 SocketRefs:20 TimesSocketAcquired:35554 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 16:08:33 mgo stats: {Clusters:0 MasterConns:9 SlaveConns:0 SentOps:40805 ReceivedOps:40795 ReceivedDocs:40795 SocketsAlive:9 SocketsInUse:10 SocketRefs:20 TimesSocketAcquired:40796 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 16:08:34 mgo stats: {Clusters:0 MasterConns:9 SlaveConns:0 SentOps:46098 ReceivedOps:46088 ReceivedDocs:46088 SocketsAlive:9 SocketsInUse:10 SocketRefs:20 TimesSocketAcquired:46089 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 16:08:35 mgo stats final: {Clusters:0 MasterConns:9 SlaveConns:0 SentOps:50009 ReceivedOps:50009 ReceivedDocs:50009 SocketsAlive:9 SocketsInUse:0 SocketRefs:0 TimesSocketAcquired:50000 TimesWaitedForPool:0 TotalPoolWaitTime:0s PoolTimeouts:0}
2021/06/03 16:08:35 >>> CLONE(50000): 9.9130598s
```

- Line #4 and #6 show nil masterSocket after dial.
  The original session don't have masterSocket ready, that means next call to Clone() will acquire socket from pool => Same behavior with copy session [2].
- Line #9 to #18 reports same behavior as copy session, 9 sockets created and being used during test duration. No pool timeout.

### 4. Compare 3 approaches

When we don't have much concurrent queries (or low load), all 3 approaches are the same, poolSize or session initialization after dial doesn't matter.

```shell
$ mgo_session --db-addrs mongo1:30001,mongo2:30002,mongo3:30003 --db-name BenchTest --db-username "" --db-password "" --worker 1 --query 30000 --db-pool-size 10 --session-first-ping
2021/06/03 16:17:30 >>> FIXED(30000): 37.7461335s
2021/06/03 16:18:13 >>> CLONE(30000): 37.8782305s
2021/06/03 16:18:55 >>> COPY(30000): 37.8921536s

$ mgo_session --db-addrs mongo1:30001,mongo2:30002,mongo3:30003 --db-name BenchTest --db-username "" --db-password "" --worker 1 --query 30000 --db-pool-size 10
2021/06/03 16:20:23 >>> FIXED(30000): 38.0221392s
2021/06/03 16:21:06 >>> CLONE(30000): 38.0048538s
2021/06/03 16:21:48 >>> COPY(30000): 37.7249939s

$ mgo_session --db-addrs mongo1:30001,mongo2:30002,mongo3:30003 --db-name BenchTest --db-username "" --db-password "" --worker 1 --query 30000 --db-pool-size 4096
2021/06/03 16:24:16 >>> FIXED(30000): 38.0556785s
2021/06/03 16:24:59 >>> CLONE(30000): 38.1186962s
2021/06/03 16:25:42 >>> COPY(30000): 38.4387756s
```

When the number of concurrent users and load increase, copy and clone (on original session with nil socket) session performs much better than fixed session (bottle neck because of not having enough socket).

```shell
$ mgo_session --db-addrs mongo1:30001,mongo2:30002,mongo3:30003 --db-name BenchTest --db-username "" --db-password "" --worker 10 --query 10000 --db-pool-size 4096
2021/06/03 19:20:11 >>> FIXED(100000): 32.9913647s
2021/06/03 19:20:41 >>> CLONE(100000): 24.7644142s
2021/06/03 19:21:09 >>> COPY(100000): 23.4504081s

$ mgo_session --db-addrs mongo1:30001,mongo2:30002,mongo3:30003 --db-name BenchTest --db-username "" --db-password "" --worker 50 --query 10000 --db-pool-size 4096
2021/06/03 19:24:31 >>> FIXED(500000): 2m20.0998102s
2021/06/03 19:25:17 >>> CLONE(500000): 41.4692296s
2021/06/03 19:26:03 >>> COPY(500000): 41.3556608s

$ mgo_session --db-addrs mongo1:30001,mongo2:30002,mongo3:30003 --db-name BenchTest --db-username "" --db-password "" --worker 100 --query 5000 --db-pool-size 4096
2021/06/03 19:29:57 >>> FIXED(500000): 2m22.8903533s
2021/06/03 19:30:38 >>> CLONE(500000): 36.3394795s
2021/06/03 19:31:20 >>> COPY(500000): 36.4537562s
```

Using clone on an original session with established socket will make the behavior and performance the same as fixed session. Be careful while using clone session and make sure you know exactly what you're doing.

```shell
$ mgo_session --db-addrs mongo1:30001,mongo2:30002,mongo3:30003 --db-name BenchTest --db-username "" --db-password "" --worker 50 --query 10000 --db-pool-size 4096 --session-first-ping
2021/06/03 19:37:55 *session-first-ping flag enabled, the original session should have masterSocket ready after calling Ping()
2021/06/03 19:40:19 >>> FIXED(500000): 2m18.8847601s
2021/06/03 19:42:43 >>> CLONE(500000): 2m18.5260889s
2021/06/03 19:43:27 >>> COPY(500000): 39.4656006s
```

Depends on the PoolLimit and contentions, copy and clone performance can suffer serious degradation. Make sure to take a good care of pool configs to cope with actual work load.

```shell
$ mgo_session --db-addrs mongo1:30001,mongo2:30002,mongo3:30003 --db-name BenchTest --db-username "" --db-password "" --worker 100 --query 5000 --db-pool-size 50
2021/06/03 20:33:27 >>> FIXED(500000): 2m27.6266815s
2021/06/03 20:34:13 >>> CLONE(500000): 40.4432206s
2021/06/03 20:34:58 >>> COPY(500000): 40.3759254s

$ mgo_session --db-addrs mongo1:30001,mongo2:30002,mongo3:30003 --db-name BenchTest --db-username "" --db-password "" --worker 100 --query 5000 --db-pool-size 10
2021/06/03 20:39:44 >>> FIXED(500000): 2m20.9347923s
2021/06/03 20:41:30 >>> CLONE(500000): 1m41.8508538s
2021/06/03 20:43:18 >>> COPY(500000): 1m42.6902571s
```

### 5. How to reproduce the tests

1. Start a local MongoDB cluster (1 primary node and 2 secondary nodes) in Docker compose: `docker-compose up` or `docker-compose up -d`.

   ```yaml
   # docker-compose.yaml
   version: "3.8"
   
   services:
     mongo1:
       image: mongo:4.2
       container_name: mongo1
       command: ["--replSet", "my-replica-set", "--bind_ip_all", "--port", "30001"]
       volumes:
         - ./data/mongo-1:/data/db
       ports:
         - 30001:30001
       healthcheck:
         test: test $$(echo "rs.initiate({_id:'my-replica-set',members:[{_id:0,host:\"mongo1:30001\"},{_id:1,host:\"mongo2:30002\"},{_id:2,host:\"mongo3:30003\"}]}).ok || rs.status().ok" | mongo --port 30001 --quiet) -eq 1
         interval: 10s
         start_period: 30s
   
     mongo2:
       image: mongo:4.2
       container_name: mongo2
       command: ["--replSet", "my-replica-set", "--bind_ip_all", "--port", "30002"]
       volumes:
         - ./data/mongo-2:/data/db
       ports:
         - 30002:30002
   
     mongo3:
       image: mongo:4.2
       container_name: mongo3
       command: ["--replSet", "my-replica-set", "--bind_ip_all", "--port", "30003"]
       volumes:
         - ./data/mongo-3:/data/db
       ports:
         - 30003:30003
   ```
   
2. Add MongoDB host to hosts file:

   ```
   127.0.0.1 mongo1
   127.0.0.1 mongo2
   127.0.0.1 mongo3
   ```

3. Build mgo_session app: `CGO_ENABLED=0 GO_OS=linux GO_ARCH=adm64 go build -o mgo_session main.go`

  ```go
// main.go
package main

import (
	"context"
	"flag"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/globalsign/mgo"
	"github.com/globalsign/mgo/bson"
)

var (
	fDbAddrs          string
	fDbName           string
	fDbUsername       string
	fDbPassword       string
	fDbPoolSize       int
	fSessionFirstPing bool

	fWorkerNum int
	fQueryNum  int
	fStats     bool

	collCipherTexts = "cipher_texts"

	testWarmUpDuration   = 5 * time.Second
	workerWarmUpDuration = 50 * time.Millisecond

	_tmpCipher *Cipher
)

type Cipher struct {
	Id         bson.ObjectId `bson:"_id" json:"id"`
	Type       string        `bson:"type,omitempty" json:"type,omitempty"`
	Name       string        `bson:"name,omitempty" json:"name,omitempty"`
	CipherText string        `bson:"cipher_text,omitempty" json:"cipher_text,omitempty"`
	CreatedAt  *time.Time    `bson:"created_at,omitempty" json:"created_at,omitempty"`
}

type MgoRepo struct {
	db *mgo.Database
}

func NewMgoRepo(db *mgo.Database) *MgoRepo {
	return &MgoRepo{
		db: db,
	}
}

func (r *MgoRepo) GetLastCipherByType_Fixed(keyType string) (cipher *Cipher, err error) {
	return queryMgo(r.db.Session, keyType)
}

func (r *MgoRepo) GetLastCipherByType_Clone(keyType string) (cipher *Cipher, err error) {
	sess := r.db.Session.Clone()
	defer sess.Close()
	return queryMgo(sess, keyType)
}

func (r *MgoRepo) GetLastCipherByType_Copy(keyType string) (cipher *Cipher, err error) {
	sess := r.db.Session.Copy()
	defer sess.Close()
	return queryMgo(sess, keyType)
}

func queryMgo(sess *mgo.Session, keyType string) (cipher *Cipher, err error) {
	cipher = new(Cipher)
	err = sess.DB("").C(collCipherTexts).
		Find(bson.M{"type": keyType}).Sort("-created_at").One(cipher)
	if err != nil {
		return nil, err
	}
	return cipher, nil
}

func (r *MgoRepo) Close() {
	if r.db != nil {
		r.db.Session.Close()
	}
}

func init() {
	flag.StringVar(&fDbAddrs, "db-addrs", "localhost:27017", "DB addresses")
	flag.StringVar(&fDbName, "db-name", "", "DB name")
	flag.StringVar(&fDbUsername, "db-username", "", "DB username")
	flag.StringVar(&fDbPassword, "db-password", "", "DB password")
	flag.IntVar(&fDbPoolSize, "db-pool-size", 4096, "DB socket pool size")
	flag.IntVar(&fWorkerNum, "worker", 10, "Number of workers")
	flag.IntVar(&fQueryNum, "query", 1000, "Number of queries per worker")
	// session-first-ping is used to determine if we should call Ping() right after Dial() or not?
	// If we call Ping after Dial, it will make the original session acquire a ready socket, which makes
	// subsequence Clone() calls on that session reuse the same underlying socket (same bahavior as fixed session [1]).
	flag.BoolVar(&fSessionFirstPing, "session-first-ping", false, "Call session.Ping() right after Dial or not")
	flag.BoolVar(&fStats, "stats", false, "Report Mgo stats")
}

func main() {
	flag.Parse()

	type getCipherFunc func(keyType string) (cipher *Cipher, err error)
	var err error
	queryFunc := func(wg *sync.WaitGroup, cipherFunc getCipherFunc) {
		defer wg.Done()

		for i := 0; i < fQueryNum; i++ {
			_tmpCipher, err = cipherFunc("N2K_INTERNAL")
			if err != nil {
				log.Panicf("failed to find cipher: %s", err)
			}
		}
	}

	wg := &sync.WaitGroup{}
	log.Printf("----- ONE FIXED SESSION -----\n")
	repo1 := getMgoRepository("benchMgoSession_FIXED")
	log.Println("wait for test...")
	time.Sleep(testWarmUpDuration)
	log.Println("test started...")
	ctxCancel1 := startStats()
	start := time.Now()
	for i := 0; i < fWorkerNum; i++ {
		wg.Add(1)
		time.Sleep(workerWarmUpDuration)
		go queryFunc(wg, repo1.GetLastCipherByType_Fixed)
	}
	wg.Wait()
	stopStats(ctxCancel1)
	repo1.Close()
	log.Printf(">>> FIXED(%d): %s\n", fWorkerNum*fQueryNum, time.Since(start))

	log.Printf("----- SESSION CLONE -----\n")
	repo2 := getMgoRepository("benchMgoSession_CLONE")
	log.Println("wait for test...")
	time.Sleep(testWarmUpDuration)
	log.Println("test started...")
	ctxCancel2 := startStats()
	start2 := time.Now()
	for i := 0; i < fWorkerNum; i++ {
		wg.Add(1)
		time.Sleep(workerWarmUpDuration)
		go queryFunc(wg, repo2.GetLastCipherByType_Clone)
	}
	wg.Wait()
	stopStats(ctxCancel2)
	repo2.Close()
	log.Printf(">>> CLONE(%d): %s\n", fWorkerNum*fQueryNum, time.Since(start2))

	log.Printf("----- SESSION COPY -----\n")
	repo3 := getMgoRepository("benchMgoSession_COPY")
	log.Println("wait for test...")
	time.Sleep(testWarmUpDuration)
	log.Println("test started...")
	ctxCancel3 := startStats()
	start3 := time.Now()
	for i := 0; i < fWorkerNum; i++ {
		wg.Add(1)
		time.Sleep(workerWarmUpDuration)
		go queryFunc(wg, repo3.GetLastCipherByType_Copy)
	}
	wg.Wait()
	stopStats(ctxCancel3)
	repo3.Close()
	log.Printf(">>> COPY(%d): %s\n", fWorkerNum*fQueryNum, time.Since(start3))

	time.Sleep(3 * time.Second) // Wait for log flush
}

func getMgoRepository(appName string) *MgoRepo {
	log.Printf("connecting to MongoDB at: %s\n", fDbAddrs)
	sess, err := mgo.DialWithInfo(&mgo.DialInfo{
		AppName:      appName,
		Addrs:        strings.Split(fDbAddrs, ","),
		Timeout:      10 * time.Second,
		Database:     fDbName,
		Username:     fDbUsername,
		Password:     fDbPassword,
		PoolLimit:    fDbPoolSize,
		PoolTimeout:  0, // Default
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	})
	if err != nil {
		log.Panicf("failed to dial MongoDB: %s", err)
	}
	sess.SetMode(mgo.Primary, true)

	if fStats {
		// session.masterSocket and session.slaveSocket should be always nil here
		log.Printf("main.getMgoRepository#beforePing: session=%#v\n", sess)
	}
	if fSessionFirstPing {
		log.Printf("*session-first-ping flag enabled, the original session should have masterSocket ready after calling Ping()")
		if err = sess.DB("").Session.Ping(); err != nil {
			log.Panicf("failed to first ping: %s", err)
		}
		// session.masterSocket and session.slaveSocket should be either or both non-nil here
		log.Printf("main.getMgoRepository#afterPing: session=%#v\n", sess)
	}

	log.Println("connected to MongoDB")
	repo := NewMgoRepo(sess.DB(""))
	if fStats {
		log.Printf("main.getMgoRepository#NewMgoRepo: repo.db.Session=%#v\n", repo.db.Session)
	}
	return repo
}

// startStats enables mgo stats collection and write it to the log every second.
func startStats() context.CancelFunc {
	if !fStats {
		return nil
	}

	mgo.SetStats(true)
	ctx, ctxCancel := context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				log.Printf("mgo stats: %+v\n", mgo.GetStats())
			}
		}
	}()

	return ctxCancel
}

// stopStats stops and resets the mgo stats collection.
func stopStats(ctxCancel context.CancelFunc) {
	if ctxCancel == nil {
		return
	}
	log.Printf("mgo stats final: %+v\n", mgo.GetStats())
	mgo.SetStats(false) // This will clean up old stats, too
	ctxCancel()
}
  ```

4. Insert some mock data into MongoDB, may need to add indexing on `type` and `created_at` field too.
```json
// A mock document in cipher_texts collection
{
  "_id" : ObjectId("5f7c162530dc040001046c28"),
  "type" : "N2K_INTERNAL",
  "name" : "my-key",
  "cipher_text" : "xoYWG5ZcAAkMaoFbcX21PMdhNFxGq1laaUkU8Dbs715uxLfyh5DfHfS8FzejaAZQ",
  "created_at" : ISODate("2021-06-02T07:00:53.338Z")
}
```
5. Run the tests
```shell
$ mgo_session --db-addrs mongo1:30001,mongo2:30002,mongo3:30003 --db-name BenchTest --db-username "" --db-password "" --worker 100 --query 5000 --db-pool-size 4096 --stats --session-first-ping
```

### 6. How to read/trace mgo library code?

- Start with the [session.go#DialInfo](https://github.com/globalsign/mgo/blob/eeefdecb41b842af6dc652aaea4026e8403e62df/session.go#L487) config struct.
- Take a look at [session.Copy()](https://github.com/globalsign/mgo/blob/eeefdecb41b842af6dc652aaea4026e8403e62df/session.go#L2035) and [session.Clone()](https://github.com/globalsign/mgo/blob/eeefdecb41b842af6dc652aaea4026e8403e62df/session.go#L2049).
- [session.One()#acquireSocket](https://github.com/globalsign/mgo/blob/eeefdecb41b842af6dc652aaea4026e8403e62df/session.go#L3691) next then its implementation [acquireSocket](https://github.com/globalsign/mgo/blob/eeefdecb41b842af6dc652aaea4026e8403e62df/session.go#L5098) where the logic of reuse original socket or acquire from pool for `Clone()` are on.
- Then [server.acquireSocketInternal](https://github.com/globalsign/mgo/blob/eeefdecb41b842af6dc652aaea4026e8403e62df/server.go#L135) where the core logic of socket pool are on.
- Last one is [stats.go](https://github.com/globalsign/mgo/blob/eeefdecb41b842af6dc652aaea4026e8403e62df/stats.go#L80). Trace the fields to see where in the code it has been called. 
  Understanding the stats will make it's a lot easier and more confidence while reading mgo_session stats (`--stats` flag) log.

