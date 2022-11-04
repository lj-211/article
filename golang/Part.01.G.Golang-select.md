# Select 的实现原理

## 带着问题读源码

T1. 为什么select的case执行是乱序的？

[Select on Golang Spec](https://go.dev/ref/spec#Select_statements)
> If one or more of the communications can proceed, a single one that can proceed is chosen via a uniform pseudo-random selection. 
>Otherwise, if there is a default case, that case is chosen. If there is no default case, the "select" statement blocks until at least one of the communications can proceed.

## 核心流程

## 核心代码

### 核心数据
``` go
type selectDir int

const (
	_             selectDir = iota
	selectSend              // case Chan <- Send
	selectRecv              // case <-Chan:
	selectDefault           // default
)

type runtimeSelect struct {
	dir selectDir
	typ unsafe.Pointer // channel type (not used here)
	ch  *hchan         // channel
	val unsafe.Pointer // ptr to data (SendDir) or ptr to receive buffer (RecvDir)
}
```

runtimeSelect描述了单个Case：
- dir send、recv or default
- ch  the channel used for case
- val data

### 核心代码

**这里我会在源码分析中留几个关键问题，看代码不是最终目的，理解设计才是目的**

``` go
func reflect_rselect(cases []runtimeSelect) (int, bool) {
	if len(cases) == 0 {
		block()
	}

    // 这里计算send & recv的数量
    // 并且重新按照 | send | send | recv | recv | default | 
    // 的方式重新构造slice
	sel := make([]scase, len(cases))
	orig := make([]int, len(cases))
	nsends, nrecvs := 0, 0
	dflt := -1
    // send case 从左边填充，recv case 从slice末尾填充
	for i, rc := range cases {
		var j int
		switch rc.dir {
		case selectDefault:
			dflt = i
			continue
		case selectSend:
			j = nsends
			nsends++
		case selectRecv:
			nrecvs++
			j = len(cases) - nrecvs
		}

		sel[j] = scase{c: rc.ch, elem: rc.val}
        // 记录case原先的位置
		orig[j] = i
	}

        // 如果没有send、recv，则select直接返回 -1， false
	// Only a default case.
	if nsends+nrecvs == 0 {
		return dflt, false
	}

        // cases中可能包含default，所以要进行一次位移
	// Compact sel and orig if necessary.
	if nsends+nrecvs < len(cases) {
		copy(sel[nsends:], sel[len(cases)-nrecvs:])
		copy(orig[nsends:], orig[len(cases)-nrecvs:])
	}

        // 这里的order是selectgo中分别用于存储
        // pollorder和lockorder的位置
	order := make([]uint16, 2*(nsends+nrecvs))
	var pc0 *uintptr
	if raceenabled {
		pcs := make([]uintptr, nsends+nrecvs)
		for i := range pcs {
			selectsetpc(&pcs[i])
		}
		pc0 = &pcs[0]
	}

        // 最后一个参数block就是由是否包含default来决定
	chosen, recvOK := selectgo(&sel[0], &order[0], pc0, nsends, nrecvs, dflt == -1)

	// Translate chosen back to caller's ordering.
	if chosen < 0 {
		chosen = dflt
	} else {
		chosen = orig[chosen]
	}
	return chosen, recvOK
}
```

selectgo是select执行的核心函数，主要包含以下几个关键步骤：

1. 重排列pollorder；**Q1 这里为什么要重排列**
```
	norder := 0
	for i := range scases {
		cas := &scases[i]

		// Omit cases without channels from the poll and lock orders.
		if cas.c == nil {
			cas.elem = nil // allow GC
			continue
		}

        // 随机一个0-norder的位置作为轮训的位置
		j := fastrandn(uint32(norder + 1))
		pollorder[norder] = pollorder[j]
		pollorder[j] = uint16(i)
		norder++
	}
```

2. 对所有的case排序，构造lockorder；**Q2 这里为什么一定要构造lockorder**
``` go

这里就是堆排序，不做详细注释，但是请思考为什么一定要构造lockorder呢?
for i := range lockorder {
		j := i
		// Start with the pollorder to permute cases on the same channel.
		c := scases[pollorder[i]].c
		for j > 0 && scases[lockorder[(j-1)/2]].c.sortkey() < c.sortkey() {
			k := (j - 1) / 2
			lockorder[j] = lockorder[k]
			j = k
		}
		lockorder[j] = pollorder[i]
	}
	for i := len(lockorder) - 1; i >= 0; i-- {
		o := lockorder[i]
		c := scases[o].c
		lockorder[i] = lockorder[0]
		j := 0
		for {
			k := j*2 + 1
			if k >= i {
				break
			}
			if k+1 < i && scases[lockorder[k]].c.sortkey() < scases[lockorder[k+1]].c.sortkey() {
				k++
			}
			if c.sortkey() < scases[lockorder[k]].c.sortkey() {
				lockorder[j] = lockorder[k]
				j = k
				continue
			}
			break
		}
		lockorder[j] = o
	}
```

3. 轮训是否有能执行的case
``` go
var casi int
var cas *scase
var caseSuccess bool
var caseReleaseTime int64 = -1
var recvOK bool
for _, casei := range pollorder {
	casi = int(casei)
	cas = &scases[casi]
	c = cas.c

    // 根据index大小判断是send还是recv
	if casi >= nsends {
        // 从chan中pop出sudog
		sg = c.sendq.dequeue()
		if sg != nil {
			goto recv
		}
		if c.qcount > 0 {
			goto bufrecv
		}
		if c.closed != 0 {
			goto rclose
		}
	} else {
		if raceenabled {
			racereadpc(c.raceaddr(), casePC(casi), chansendpc)
		}
		if c.closed != 0 {
			goto sclose
		}
        // 从recv queue中pop出sudog
		sg = c.recvq.dequeue()
		if sg != nil {
			goto send
		}
		if c.qcount < c.dataqsiz {
			goto bufsend
		}
	}
}

// 如果执行到这里，没有要执行的case，并且有default分支，则直接去执行retc label
if !block {
	selunlock(scases, lockorder)
	casi = -1
	goto retc
}
```

4. 重新构造sudog，放到每个chan的waitq
``` go
// pass 2 - enqueue on all chans
gp = getg()
if gp.waiting != nil {
	throw("gp.waiting != nil")
}
nextp = &gp.waiting
for _, casei := range lockorder {
	casi = int(casei)
	cas = &scases[casi]
	c = cas.c
	sg := acquireSudog()
	sg.g = gp
	sg.isSelect = true
	// No stack splits between assigning elem and enqueuing
	// sg on gp.waiting where copystack can find it.
	sg.elem = cas.elem
	sg.releasetime = 0
	if t0 != 0 {
		sg.releasetime = -1
	}
	sg.c = c
	// Construct waiting list in lock order.
	*nextp = sg
	nextp = &sg.waitlink

	if casi < nsends {
		c.sendq.enqueue(sg)
	} else {
		c.recvq.enqueue(sg)
	}
}

// wait for someone to wake us up
gp.param = nil
// Signal to anyone trying to shrink our stack that we're about
// to park on a channel. The window between when this G's status
// changes and when we set gp.activeStackChans is not safe for
// stack shrinking.
gp.parkingOnChan.Store(true)
gopark(selparkcommit, nil, waitReasonSelect, traceEvGoBlockSelect, 1)
gp.activeStackChans = false

sellock(scases, lockorder)

gp.selectDone.Store(0)
sg = (*sudog)(gp.param)
gp.param = nil
```
- 4.1 构造完sudog后，结构如图所示
```
┌───────┐              ┌───────────────┐
│       │waiting       │               │
│   G   ├─────────────►│    SudoG      │
│       │              │               │
└───────┘              └───────┬───────┘
                               │
                               │ waitlink
                               │
                               │
                       ┌───────▼───────┐
                       │               │
                       │    SudoG      │
                       │               │
                       └───────┬───────┘
                               │
                               │
                               │
                               │
                       ┌───────▼───────┐
                       │               │
                       │    SudoG      │
                       │               │
                       └───────────────┘
```


- 4.2 向send/recv的wait list中塞入sudog

- 4.3 gopark 当前g

5. 被唤醒后，比对sudog，来查出来是哪个case被唤醒；其他的case要把他们的sudog从对应的chan的queue中移除
``` go
// pass 3 - dequeue from unsuccessful chans
// otherwise they stack up on quiet channels
// record the successful case, if any.
// We singly-linked up the SudoGs in lock order.
casi = -1
cas = nil
caseSuccess = false
sglist = gp.waiting
// Clear all elem before unlinking from gp.waiting.
for sg1 := gp.waiting; sg1 != nil; sg1 = sg1.waitlink {
	sg1.isSelect = false
	sg1.elem = nil
	sg1.c = nil
}
gp.waiting = nil

for _, casei := range lockorder {
	k = &scases[casei]
	if sg == sglist {
		// sg has already been dequeued by the G that woke us up.
		casi = int(casei)
		cas = k
		caseSuccess = sglist.success
		if sglist.releasetime > 0 {
			caseReleaseTime = sglist.releasetime
		}
	} else {
		c = k.c
		if int(casei) < nsends {
			c.sendq.dequeueSudoG(sglist)
		} else {
			c.recvq.dequeueSudoG(sglist)
		}
	}
	sgnext = sglist.waitlink
	sglist.waitlink = nil
	releaseSudog(sglist)
	sglist = sgnext
}
```

退出函数的几个label比较简单，在这里简单描述下

``` go
bufrecv:
    1. move data to cas.elem
    2. goto retc
bufsend:
    1. 向chan的发送queue拷贝数据
    2. goto retc
recv: 
    1. 调用chan的recv函数收取数据
    2. 赋值返回值recvOK
    3. goto retc
rclose:
    1. 读到已关闭的chan(当前g被chan唤醒)
    2. goto retc
send: 调用chan的send函数发送数据
retc: 返回case index以及recv状态
sclose: 写已关闭的chan，直接panic
```

## Questions

Q1 这里为什么要重排列

对于select来说，除了default，其他的case都是chan,我们假设这些chan都是block的；
在这种假设的前提下，如果是固定的顺序，会发生什么？

极端情况，poll顺序的第一个永远block，即使后面的case有读写机会。

这就有点类似于epoll的starvation pitfall。

所以最终目的是保证公平、防止饥饿。

Q2 这里为什么一定要构造lockorder

``` go

func test() {
    chan1 := make(chan bool)
    chan2 := make(chan bool)

    go func() {
        select {
            case <- chan1:
                fmt.Println("one")
            case <- chan2:
                fmt.Println("two")
        }
    }()

    go func() {
        select {
            case <- chan1:
                fmt.Println("one")
            case <- chan2:
                fmt.Println("two")
        }
    }()

    time.Sleep(time.Second * 2)
    chan1 <- true 
    chan1 <- true 
}
```

如果不能按照一定顺序lock，那么对于chan1、chan2的lock，如果顺序正好是相反的，那很可能发生死锁。

## Further thinking
1. 为什么lockorder需要按照地址排序？

考虑这种情况：

``` go

func test() {
    chan1 := make(chan bool)

    go func() {
        select {
            case <- chan1:
                fmt.Println("one")
            case <- chan1:
                fmt.Println("two")
        }
    }()
}

```

这段代码可以正常编译以及执行，那么问题来了，怎么防止重复lock?

所以明白了，用地址排序有什么好处了吗，在代码中可以保留个lastc，和当前c比对来避免重复lock。
