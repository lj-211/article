# Go的sync.map的源码分析
## 基本概念
entry: map的一个slot对应的值，包含了key对应的值
expunged: 如果一个entry为expunged表示这个key对应的值，在dirty map中不存在了

dirtymap到readmap的提升策略 - 根据Load没有从readmap命中的次数决定
readmap到dirtymap的拷贝过程 - 不拷贝nil的entry，并且会把他们在readmap中置为expunged

## 读过程
```
func (m *Map) Load(key interface{}) (value interface{}, ok bool) {
	read, _ := m.read.Load().(readOnly)
	e, ok := read.m[key]
	// 如果没有找到并且有修改则去dirymap中找
	if !ok && read.amended {
		m.mu.Lock()
		// 二次确认，因为有可能有并发的触发dirtymap提升为readmap
		read, _ = m.read.Load().(readOnly)
		e, ok = read.m[key]
		if !ok && read.amended {
			e, ok = m.dirty[key]
			// 触发提升策略
			m.missLocked()
		}
		m.mu.Unlock()
	}
	if !ok {
		return nil, false
	}
	return e.load()
}

func (m *Map) missLocked() {
	// 命中次数++
	m.misses++
	if m.misses < len(m.dirty) { // 没有满足
		return
	}
	// 将dirtymap赋值给readmap
	m.read.Store(readOnly{m: m.dirty})
	// 清理&重新计数
	m.dirty = nil
	m.misses = 0
}
```

## 写过程

```
// 尝试把对非expunged的key更新，如果失败什么都不做
func (e *entry) tryStore(i *interface{}) bool {
	for {
		p := atomic.LoadPointer(&e.p)
		if p == expunged {
			return false
		}
		if atomic.CompareAndSwapPointer(&e.p, p, unsafe.Pointer(i)) {
			return true
		}
	}
}

func (m *Map) dirtyLocked() {
	if m.dirty != nil {
		return
	}

	// 拷贝readmap数据
	// 过程中丢弃已删除元素，并将其设置为expunged，这就是为什么元素为expunged的则dirtymap不为空的保证
	read, _ := m.read.Load().(readOnly)
	m.dirty = make(map[interface{}]*entry, len(read.m))
	for k, e := range read.m {
		if !e.tryExpungeLocked() {
			m.dirty[k] = e
		}
	}
}

func (m *Map) Store(key, value interface{}) {
	read, _ := m.read.Load().(readOnly)
	if e, ok := read.m[key]; ok && e.tryStore(&value) {
		return
	}

	m.mu.Lock()
	read, _ = m.read.Load().(readOnly)
	if e, ok := read.m[key]; ok {
		if e.unexpungeLocked() {
			// 这个entry之前被设置为expunged,这说明dirtymap肯定不为空，并且
			// 这个key在dirtymap中不存在，所以先在dirtymap中插入
			m.dirty[key] = e
		}
		// 更新readmap中的entry
		e.storeLocked(&value)
	} else if e, ok := m.dirty[key]; ok {
		// 找到则更新
		e.storeLocked(&value)
	} else {
		if !read.amended {
			// 要在dirtymap中插入元素，所以新建dirtymap然后，拷贝readmap中的entry
			// 这里有个特殊的点，如果是nil,则置为expunged 
			// 不会往dirtymap赋值
			// 并且修改readmap为有变化
			m.dirtyLocked()
			m.read.Store(readOnly{m: read.m, amended: true})
		}
		m.dirty[key] = newEntry(value)
	}
	m.mu.Unlock()
}
```

## 图解
如果你对上面的源码理解有困难，请参照以下图解
```
                 +----------------+--------+          +------------------+
                 |     readmap    |  amend |          |     dirtymap     |        miss: 0
                 +-------------------------+          +------------------+
                                  |  false |
                                  +--------+
store("1", a)    +-------------------------+          +------------------+        miss: 0
                 |     readmap    |  amend |          |     dirtymap     |
                 +-------------------------+          +------------------+
                                  |  true  |          |    1   :    a    |
                                  +--------+          +------------------+
store("2", b)    +-------------------------+          +------------------+        miss: 0
                 |     readmap    |  amend |          |     dirtymap     |
                 +-------------------------+          +------------------+
                                  |  true  |          |    1   :    a    |
                                  +--------+          +------------------+
                                                      |    2   :    b    |
                                                      +------------------+
load("1")        +----------------+--------+          +------------------+        miss: 1
                 |     readmap    |  amend |          |     dirtymap     |
                 +-------------------------+          +------------------+
                                  |  true  |          |    1   :    a    |
                                  +--------+          +------------------+
                                                      |    2   :    b    |
                                                      +------------------+
load("1")        +----------------+--------+          +------------------+
                 |     readmap    |  amend |          |     dirtymap     |        miss: 2      premote dirtymap
                 +-------------------------+          +------------------+
                                  |  true  |          |    1   :    a    |
                                  +--------+          +------------------+
                                                      |    2   :    b    |
                                                      +------------------+
                 +----------------+--------+          +------------------+
                 |     readmap    |  amend |          |     dirtymap     |        miss: 0
                 +-------------------------+          +------------------+
                 |    1   :    a  |        |
                 +----------------+  false |
                 |    2   :    b  |        |
                 +----------------+--------+

delete("1")      +----------------+--------+          +------------------+
                 |     readmap    |  amend |          |     dirtymap     |        miss: 0
                 +-------------------------+          +------------------+
                 |    1   :  nil  |        |
                 +----------------+  false |
                 |    2   :    b  |        |
                 +----------------+--------+

                 +-------------------------+          +------------------+
store("3", c)    |     readmap      |amend |          |     dirtymap     |        miss: 0       copy readmap to dirymap
                 +-------------------------+          +------------------+
                 |    1   : expunged|      |          |    3   :    c    |
                 +------------------+false |          +------------------+
                 |    2   :    b    |      |          |    2   :    b    |
                 +------------------+------+          +------------------+

                 +-------------------------+          +------------------+
store("1", a)    |     readmap      |amend |          |     dirtymap     |        miss: 0       expunged make sure diry map is not nil
                 +-------------------------+          +------------------+
                 |    1   :    a    |      |          |    3   :    c    |
                 +------------------+false |          +------------------+
                 |    2   :    b    |      |          |    2   :    b    |
                 +------------------+------+          +------------------+
                                                      |    1   :    a    |
                                                      +------------------+
```
