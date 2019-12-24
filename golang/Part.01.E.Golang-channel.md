# Golang - Channel
TODO: 
- sudog的结构

```
type hchan struct {
	qcount   uint           // 队列元素长度
	dataqsiz uint           // 环形队列长度
	buf      unsafe.Pointer // 环形队列的数组
	elemsize uint16			// 环形队列中的元素大小
	closed   uint32			// 是否关闭标志位
	elemtype *_type // element type
	sendx    uint   // 发送位置	
	recvx    uint   // 接受位置
	recvq    waitq  // 读等待者列表
	sendq    waitq  // 写等待者列表

	// lock protects all fields in hchan, as well as several
	// fields in sudogs blocked on this channel.
	//
	// Do not change another G's status while holding this lock
	// (in particular, do not ready a G), as this can deadlock
	// with stack shrinking.
	lock mutex
}

// sudog是对于g在等待列表中的封装
// sudog是有必要的是因为，g的同步关系是多对多的。一个g可能在多个等待队列
// 这里只展示跟channel相关度高的几个成员
type sudog struct {
	g *g 				// 协程

	......

	elem     unsafe.Pointer // 数据元素

	......

	c           *hchan // channel
}
```
## 背景知识
如果你熟悉go的协程调度知识这部分可以直接跳过

goparkunlock 把当前的协程从running->wait状态
goready	唤醒协程
acquireSudog 从一个特殊的池中分配一个sudog
releaseSudog 释放sudog


## 创建channel
创建channel的代码逻辑比较简单，这里不列出源码，简单说下流程
1. 类型检查以及对齐检查 
2. 内存大小溢出检查
3. 分配内存以及设置结构体变量
```
func send(c *hchan, sg *sudog, ep unsafe.Pointer, unlockf func(), skip int) {
	
	......

	// 如果sudog的发送数据不为空，则直接拷贝数据
	if sg.elem != nil {
		sendDirect(c.elemtype, sg, ep)
		sg.elem = nil
	}
	// 获取等待的协程
	gp := sg.g
	unlockf()
	gp.param = unsafe.Pointer(sg)
	// 阻塞时间统计
	if sg.releasetime != 0 {
		sg.releasetime = cputicks()
	}
	// 唤醒写协程
	goready(gp, skip+1)
}

func chansend(c *hchan, ep unsafe.Pointer, block bool, callerpc uintptr) bool {
	if c == nil {
		// 不是阻塞就直接返回
		if !block {
			return false
		}
		// 这里和写closed的channel是不一样的
		// 因为chan send (nil chan)挂起协程，这里就永远被挂起了
		gopark(nil, nil, waitReasonChanSendNilChan, traceEvGoStop, 2)
		throw("unreachable")
	}

	......

	// 快递排查非阻塞直接返回的情况
	// 非阻塞 && 未关闭 && (队列大小为0且没有等待者) && (队列大小大于0且队列已经满了)
	if !block && c.closed == 0 && ((c.dataqsiz == 0 && c.recvq.first == nil) ||
		(c.dataqsiz > 0 && c.qcount == c.dataqsiz)) {
		return false
	}

	// 协程阻塞，cpu打点
	var t0 int64
	if blockprofilerate > 0 {
		t0 = cputicks()
	}

	lock(&c.lock)

	if c.closed != 0 {
		unlock(&c.lock)
		panic(plainError("send on closed channel"))
	}

	// 等待队列取一个等待者发送数据
	if sg := c.recvq.dequeue(); sg != nil {
		// Found a waiting receiver. We pass the value we want to send
		// directly to the receiver, bypassing the channel buffer (if any).
		send(c, sg, ep, func() { unlock(&c.lock) }, 3)
		return true
	}

	// 如果还有容量
	if c.qcount < c.dataqsiz {
		// 压入buffer中待读取
		qp := chanbuf(c, c.sendx)

		......

		// 拷贝数据，发送索引递增&roundup
		typedmemmove(c.elemtype, qp, ep)
		c.sendx++
		if c.sendx == c.dataqsiz {
			c.sendx = 0
		}
		// 元素计数递增
		c.qcount++
		unlock(&c.lock)
		return true
	}

	// 非阻塞返回false
	if !block {
		unlock(&c.lock)
		return false
	}


	// Block on the channel. Some receiver will complete our operation for us.
	// 当前协程阻塞在这个channel
	gp := getg()
	mysg := acquireSudog()
	mysg.releasetime = 0
	if t0 != 0 {
		mysg.releasetime = -1
	}
	
	// 把sudog放到写队列中
	mysg.elem = ep
	mysg.waitlink = nil
	mysg.g = gp
	mysg.isSelect = false
	mysg.c = c
	gp.waiting = mysg
	gp.param = nil
	c.sendq.enqueue(mysg)

	// 休眠当前协程
	goparkunlock(&c.lock, waitReasonChanSend, traceEvGoBlockSend, 3)

	// TODO: 为什么唤醒后没有写ep的过程
	// 确保ep不被gc
	KeepAlive(ep)

	// 被唤醒
	if mysg != gp.waiting {
		throw("G waiting list is corrupted")
	}
	gp.waiting = nil
	// 为空则说明channel已经被关闭了
	if gp.param == nil {
		if c.closed == 0 {
			throw("chansend: spurious wakeup")
		}
		// 唤醒之后channel被关闭了，直接panic
		panic(plainError("send on closed channel"))
	}
	gp.param = nil
	// 计算阻塞时间
	if mysg.releasetime > 0 {
		blockevent(mysg.releasetime-t0, 2)
	}
	mysg.c = nil
	releaseSudog(mysg)
	return true
}
```

### 读取channel
```
func recv(c *hchan, sg *sudog, ep unsafe.Pointer, unlockf func(), skip int) {
	if c.dataqsiz == 0 {
		......

		// 队列为空的情况，直接从往sudog拷贝ep
		if ep != nil {
			// copy data from sender
			recvDirect(c.elemtype, sg, ep)
		}
	} else {
		// 去读取索引对应的数据
		qp := chanbuf(c, c.recvx)
		
		......

		// 从队列拷贝数据到ep
		if ep != nil {
			typedmemmove(c.elemtype, ep, qp)
		}
		// copy data from sender to queue
		typedmemmove(c.elemtype, qp, sg.elem)
		// 计数以及roundup
		c.recvx++
		if c.recvx == c.dataqsiz {
			c.recvx = 0
		}
		c.sendx = c.recvx // c.sendx = (c.sendx+1) % c.dataqsiz
	}
	sg.elem = nil
	gp := sg.g
	unlockf()
	// 设置协程的param为sudog
	gp.param = unsafe.Pointer(sg)
	if sg.releasetime != 0 {
		sg.releasetime = cputicks()
	}
	// 唤醒协程
	goready(gp, skip+1)
}

func chanrecv(c *hchan, ep unsafe.Pointer, block bool) (selected, received bool) {
	
	// 这里和send比较类似
	if c == nil {
		if !block {
			return
		}
		// 休眠原因 "chan receive (nil chan)"
		gopark(nil, nil, waitReasonChanReceiveNilChan, traceEvGoStop, 2)
		throw("unreachable")
	}

	// 这里要注意的是检查顺序不可颠倒，必须在最后检查channel关闭状态
	// 因为有可能检查之后，已经被关闭了
	if !block && (c.dataqsiz == 0 && c.sendq.first == nil ||
		c.dataqsiz > 0 && atomic.Loaduint(&c.qcount) == 0) &&
		atomic.Load(&c.closed) == 0 {
		return
	}

	var t0 int64
	if blockprofilerate > 0 {
		t0 = cputicks()
	}

	lock(&c.lock)

	// 被关闭且没有数据可读（这里就是可以读取关闭channel的原因）
	if c.closed != 0 && c.qcount == 0 {
		if raceenabled {
			raceacquire(c.raceaddr())
		}
		unlock(&c.lock)
		if ep != nil {
			typedmemclr(c.elemtype, ep)
		}
		return true, false
	}

	// 取出发送队列sudog直接拷贝数据
	if sg := c.sendq.dequeue(); sg != nil {
		recv(c, sg, ep, func() { unlock(&c.lock) }, 3)
		return true, true
	}

	// 数据队列中有数据直接拷贝数据
	if c.qcount > 0 {
		qp := chanbuf(c, c.recvx)
		if raceenabled {
			raceacquire(qp)
			racerelease(qp)
		}
		if ep != nil {
			typedmemmove(c.elemtype, ep, qp)
		}
		typedmemclr(c.elemtype, qp)
		c.recvx++
		if c.recvx == c.dataqsiz {
			c.recvx = 0
		}
		c.qcount--
		unlock(&c.lock)
		return true, true
	}

	// 没有数据可读非阻塞则返回
	if !block {
		unlock(&c.lock)
		return false, false
	}

	// 阻塞模式下阻塞当前协程
	gp := getg()
	mysg := acquireSudog()
	mysg.releasetime = 0
	if t0 != 0 {
		mysg.releasetime = -1
	}
	// No stack splits between assigning elem and enqueuing mysg
	// on gp.waiting where copystack can find it.
	mysg.elem = ep
	mysg.waitlink = nil
	gp.waiting = mysg
	mysg.g = gp
	mysg.isSelect = false
	mysg.c = c
	gp.param = nil
	c.recvq.enqueue(mysg)
	goparkunlock(&c.lock, waitReasonChanReceive, traceEvGoBlockRecv, 3)

	// 被唤醒
	if mysg != gp.waiting {
		throw("G waiting list is corrupted")
	}
	gp.waiting = nil
	if mysg.releasetime > 0 {
		blockevent(mysg.releasetime-t0, 2)
	}
	// 在关闭channel的时候会把gp.param置为nil
	// 见recv函数下的逻辑: 非关闭下唤醒gp.param不为空
	closed := gp.param == nil
	gp.param = nil
	mysg.c = nil
	releaseSudog(mysg)
	return true, !closed
}
```

### 关闭channel
```
func closechan(c *hchan) {
	// 关闭空channle会panic
	if c == nil {
		panic(plainError("close of nil channel"))
	}

	lock(&c.lock)
	// 关闭已关闭channel会panic
	if c.closed != 0 {
		unlock(&c.lock)
		panic(plainError("close of closed channel"))
	}

	......

	c.closed = 1

	var glist gList

	// release all readers
	for {
		sg := c.recvq.dequeue()
		if sg == nil {
			break
		}
		if sg.elem != nil {
			typedmemclr(c.elemtype, sg.elem)
			sg.elem = nil
		}
		if sg.releasetime != 0 {
			sg.releasetime = cputicks()
		}
		gp := sg.g
		gp.param = nil
		if raceenabled {
			raceacquireg(gp, c.raceaddr())
		}
		glist.push(gp)
	}

	// 释放所有写等待的协程，他们会panic，对应chansend中的唤醒那部分判断
	for {
		sg := c.sendq.dequeue()
		if sg == nil {
			break
		}
		sg.elem = nil
		if sg.releasetime != 0 {
			sg.releasetime = cputicks()
		}
		gp := sg.g
		gp.param = nil
		if raceenabled {
			raceacquireg(gp, c.raceaddr())
		}
		glist.push(gp)
	}
	unlock(&c.lock)

	// 唤醒所有的读写协程
	for !glist.empty() {
		gp := glist.pop()
		gp.schedlink = 0
		goready(gp, 3)
	}
}
```

## tips
看了以上代码，那么对channel进行操作时要注意以下几点

- nil的channel 如果是阻塞读写可能导致协程永远挂起
- 写已关闭的channel会直接panic
- 读已关闭的channel，如果有数据则正常返回，如果没有数据则返回0
- 关闭同一个channel两次会导致panic
