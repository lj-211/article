# Go的Mutex和RWMutex实现源码分析
## Mutex
### 前置知识
源码中有部分runtime函数，需要提前知道他们的作用才能更好理解mutex的实现
1. runtime_SemacquireMutex(&m.sema, queueLifo) 挂起当前协程到,m.sema对应的队列 queueInfo true LIFO  false FIFO
2. runtime_Semrelease(&m.sema, true) handoff为真表示直接传递给队首协程

### 基础概念
#### mode
- 饥饿模式
- 普通模式

这两个模式有几个典型特征，这几个特征在后续的代码中都会有体现：
1. 饥饿模式对等于mutex状态是锁定并且必须有等待者
2. 饥饿模式下没有挂起的协程，不允许抢锁，直接进入等待队列
3. 被唤醒的协程，如果处于饥饿模式，这个情况下的代码是并发安全运行的

后面的逻辑会先从解锁逻辑开始梳理

#### 位操作
```
32 31 ... 4 3 2 1
 |--------| | | |
          |-|-|-|------> 等待协程数
            |-|-|------> 是否处于饥饿模式
              |-|------> 是否有协程处于唤醒状态
                |------> 是否锁定状态
```

1<<mutexWaiterShift: 一个等待单位

old>>mutexWaiterShift: 当前等待的协程

### 解锁
``` go
    // 1. 锁定标志位只能被第一个Unlock的协程调用的，所以这里放心的做减法
    // 从这里可以看出，go的mutex是跨协程的
	new := atomic.AddInt32(&m.state, -mutexLocked)
	// 2. unlock没有锁定的mutex是异常的
	if (new+mutexLocked)&mutexLocked == 0 {
		throw("sync: unlock of unlocked mutex")
	}
	if new&mutexStarving == 0 { // 3. 当前并非出于饥饿模式
		old := new
		for {
		    // 4. 当前没有等待协程 || （锁已被获取或者有协程苏醒或者进入饥饿模式）
		    //          直接返回，不需要做任何唤醒操作
			if old>>mutexWaiterShift == 0 || old&(mutexLocked|mutexWoken|mutexStarving) != 0 {
				return
			}
			// 5. 这里为什么要把等待数量-1
			//  因为这里是非饥饿模式，而饥饿模式则是在Lock里面进行减1
			//  非饥饿模式下，则是在lock代码中直接进入下一个循环，所以只能
			//  在这里减1操作
			new = (old - 1<<mutexWaiterShift) | mutexWoken
			if atomic.CompareAndSwapInt32(&m.state, old, new) {
				runtime_Semrelease(&m.sema, false)
				return
			}
			old = m.state
		}
	} else {
	    // 6. 如果是饥饿模式，则唤醒等待协程队列的第一个
		runtime_Semrelease(&m.sema, true)
	}
```

### Lock
锁定的流程要分为两部分看，部分关键点注释是以k开头的注释；关键点解释如下:

- k1: 这个关键点是个切分，上面是处于唤醒状态或者新来的协程执行，下面的是被唤醒后执行
- k2: 如果处于饥饿状态，则后面的代码是没有并发执行的（这里要想通，因为饥饿模式下，其他协程会排队挂起，而唤醒这个协程是在unlock里面做的）
- k3: 这里有点迷惑人，因为判断有没有抢到锁是在k4这里执行
- k4: 前置状态不包含锁定或者饥饿则证明抢锁成功，为什么饥饿也不行，因为k3那里饥饿是不会设定锁定状态的
- k5: 这里通过等待时间判断是新来的协程还是被唤醒的协程

mutexWoken这个状态位会导致新协程抢夺锁更有优势。

``` go
func (m *Mutex) Lock() {
	// Fast path: grab unlocked mutex.
	if atomic.CompareAndSwapInt32(&m.state, 0, mutexLocked) {
		if race.Enabled {
			race.Acquire(unsafe.Pointer(m))
		}
		return
	}

	var waitStartTime int64
	starving := false
	awoke := false
	iter := 0
	old := m.state
	for {
		// Don't spin in starvation mode, ownership is handed off to waiters
		// so we won't be able to acquire the mutex anyway.
		// 只有锁定+普通模式才进行自旋
		// 自旋的同时，把唤醒标志位打开，表示目前有协程准备抢锁
		if old&(mutexLocked|mutexStarving) == mutexLocked && runtime_canSpin(iter) {
			// Active spinning makes sense.
			// Try to set mutexWoken flag to inform Unlock
			// to not wake other blocked goroutines.
			if !awoke && old&mutexWoken == 0 && old>>mutexWaiterShift != 0 &&
				atomic.CompareAndSwapInt32(&m.state, old, old|mutexWoken) {
				awoke = true
			}
			runtime_doSpin()
			iter++
			old = m.state
			continue
		}
		new := old
		// 非饥饿模式尝试设定锁定位
		// k3: 这里设定标志位不代表抢到锁，因为本来这个位置就可能已设定
		if old&mutexStarving == 0 {
			new |= mutexLocked
		}
		// 锁定或者饥饿下，都没有机会抢锁，直接排队+1
		if old&(mutexLocked|mutexStarving) != 0 {
			new += 1 << mutexWaiterShift
		}
		// 当前锁定并且协程处于饥饿状态则设定mutex为饥饿状态
		if starving && old&mutexLocked != 0 {
			new |= mutexStarving
		}
		// 即将进入要么抢锁成功要么挂起的状态，所以去除woken标志位
		if awoke {
			// The goroutine has been woken from sleep,
			// so we need to reset the flag in either case.
			if new&mutexWoken == 0 {
				throw("sync: inconsistent mutex state")
			}
			new &^= mutexWoken
		}
		if atomic.CompareAndSwapInt32(&m.state, old, new) { // 设定成功
		    // k4: 锁定成功
			if old&(mutexLocked|mutexStarving) == 0 {
				break // locked the mutex with CAS
			}
			// 老状态处于锁定或者饥饿模式，则说明new中没有锁定状态，即为锁定失败
			// If we were already waiting before, queue at the front of the queue.
			// k5: 这里有个逻辑是如果是老协程，则直接塞回队首
			queueLifo := waitStartTime != 0
			if waitStartTime == 0 {
				waitStartTime = runtime_nanotime()
			}
		
		    // k1: 挂起当前协程
			runtime_SemacquireMutex(&m.sema, queueLifo)
			// k2: 协程被唤醒
			starving = starving || runtime_nanotime()-waitStartTime > starvationThresholdNs
			old = m.state
			if old&mutexStarving != 0 { // 当前处于饥饿模式，则当前没有人抢锁，直接执行
				// 不可能存在状态包含锁定和唤醒状态 或者 没有等待者(进入饥饿模式的条件是有等待者)
				// 饥饿模式下，新来的协程不会设定唤醒标志
				if old&(mutexLocked|mutexWoken) != 0 || old>>mutexWaiterShift == 0 {
					throw("sync: inconsistent mutex state")
				}
				// 去除相关标志位，如果不饥饿或者没人等待则离开饥饿模式
				delta := int32(mutexLocked - 1<<mutexWaiterShift)
				if !starving || old>>mutexWaiterShift == 1 {
					// Exit starvation mode.
					// Critical to do it here and consider wait time.
					// Starvation mode is so inefficient, that two goroutines
					// can go lock-step infinitely once they switch mutex
					// to starvation mode.
					delta -= mutexStarving
				}
				atomic.AddInt32(&m.state, delta)
				break
			}
			awoke = true
			iter = 0
		} else { // 设定失败，说明老状态有变化，刷新状态
			old = m.state
		}
	}
}
```

## RWMutex
RWMutex的实现远比Mutex的实现要简单的多，这里主要对源码注释进行解析。
### RLock
``` go
func (rw *RWMutex) RLock() {
    // 这里判断小于0，是因为后面Lock的时候把readerCount-rwmutexMaxReaders了
	if atomic.AddInt32(&rw.readerCount, 1) < 0 {
		// A writer is pending, wait for it.
		runtime_SemacquireMutex(&rw.readerSem, false)
	}
}
```
### RUnlock
``` go
func (rw *RWMutex) RUnlock() {
	if r := atomic.AddInt32(&rw.readerCount, -1); r < 0 {
	    // 没有readerCount所以这了直接抛异常
		if r+1 == 0 || r+1 == -rwmutexMaxReaders {
			race.Enable()
			throw("sync: RUnlock of unlocked RWMutex")
		}
		// readerWait 表示写锁等待的读协程
		// 在Lock的流程中，会把当前的readerCount写入readerWait
		if atomic.AddInt32(&rw.readerWait, -1) == 0 {
			// The last reader unblocks the writer.
			runtime_Semrelease(&rw.writerSem, false)
		}
	}
}
```
### Lock
``` go
func (rw *RWMutex) Lock() {
    // 抢写锁
	rw.w.Lock()
	// 这里直接把readerCount减到最小值
	r := atomic.AddInt32(&rw.readerCount, -rwmutexMaxReaders) + rwmutexMaxReaders
	// 写入readerWait数量，以便在RUnlock的时候可以得到通知
	if r != 0 && atomic.AddInt32(&rw.readerWait, r) != 0 {
		runtime_SemacquireMutex(&rw.writerSem, false)
	}
}
```
### Unlock
``` go
func (rw *RWMutex) Unlock() {
	// 恢复readerCount到正常值
	r := atomic.AddInt32(&rw.readerCount, rwmutexMaxReaders)
	if r >= rwmutexMaxReaders {
		race.Enable()
		throw("sync: Unlock of unlocked RWMutex")
	}
	// 唤醒挂起的读协程
	for i := 0; i < int(r); i++ {
		runtime_Semrelease(&rw.readerSem, false)
	}
	// Allow other writers to proceed.
	rw.w.Unlock()
}
```
