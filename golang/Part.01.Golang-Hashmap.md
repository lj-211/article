## 背景知识
golang的map的实现是一个hashmap,对于hashmap来说最关键的两个key point:
1. hash函数
2. 桶溢出处理
3. 负载扩容

buckets: 桶数组
bucket: 单个桶
cell: 桶中的元素
overflow: 单个桶的元素满了后，会溢出一个新的bucket，形成bucket链表

### hash函数
对于key值的hash过程如下:
```
           +----------------------------------------------------------+
           |                        32位                               |
           +-----------+------------------------------+---------------+
           |           |                              |               |
   +-------+---+8位 +---+                              +<------------->+----> locate bucket
   |          tophash                                        B位
   v
locate cell
```

### 扩容
golang的hashtable的实现扩容有两个基本概念
1. 等量扩容 cell过于稀疏无法触发负载因子，但是又有很多利用率不高的overflow bucket。
2. 增量扩容 元素/bucket数量超过负载因子，需要扩容，而扩容的过程是增量扩容，碰到写和删会触发增量扩容逻辑。

## 数据结构
> 常用标量变量
```
const (
	// 单个bucket存储的元素数量
	bucketCntBits = 3
	bucketCnt     = 1 << bucketCntBits

	// loadFactorNum/loadFactDen 表示装载因子，超过这个就需要扩容
	// loadFactorNum * (2^B) / loadFactDen 大于这个值就是超过负载
	loadFactorNum = 13
	loadFactorDen = 2

	// 这个大小以下的k & v是可以inline
	maxKeySize   = 128
	maxValueSize = 128

	// bmap数据偏移
	dataOffset = unsafe.Offsetof(struct {
		b bmap
		v int64
	}{}.v)

	// tophash中的标志位
	emptyRest      = 0 // 从当前cell开始后都是空的
	emptyOne       = 1 // 当前cell是空的
	evacuatedX     = 2 // 翻倍扩容后(B+1)因为掩码多了位0,保留在原位置
	evacuatedY     = 3 // 翻倍扩容后(B+1)因为掩码多了位1,位置增加了2^B
	evacuatedEmpty = 4 // bucket已经迁移完成
	minTopHash     = 5 // hash值的偏移，小于这个值的用于作为标志位

	// flags
	iterator     = 1 // 有迭代器在使用buckets
	oldIterator  = 2 // 有迭代器在使用oldbuckets
	hashWriting  = 4 // 有协程正在写map
	sameSizeGrow = 8 // 同等大小扩容(没有超过负载因子，但是overflow的buckets太多，碎片太多)

	// sentinel bucket ID for iterator checks
	noCheck = 1<<(8*sys.PtrSize) - 1	// 用于标志不需要进行检查
)
```

> hashmap的结构
```
type hmap struct {
	count     int // map的元素个数
	flags     uint8	// 状态标志位
	B         uint8  // bucket数量的log_2 (最多可以保存 装载因子 * 2^B 元素)
	noverflow uint16 // overflow的bucket数量，量级小的时候精确，大的时候是近似值
	hash0     uint32 // hash种子

	buckets    unsafe.Pointer // hash桶的起始指针
	oldbuckets unsafe.Pointer // 扩容的时候才不为空，老的hash桶指针 是否为空作为判断是否扩容的依据
	nevacuate  uintptr        // 小于这个索引前的bucket都已经迁移完成

	extra *mapextra // 为了避免被gc,需要单独hold溢出桶的指针
}
```

> hash桶的结构
```
// A bucket for a Go map.
type bmap struct {
	// 8个8位的值，不为空的时候表示hash值的前8位，为空的时候表示迁移状态
	tophash [bucketCnt]uint8
	// bmap的结构没有那么直观，在tophash后面还跟有k/v以及溢出桶的指针
	// 详情见下面图解
}

               +---+---+---+---+---+---+---+---+
tophash ------>+ 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 |
               +-------------------------------+
               | k1| k2| k3| k4| k5| k6| k7| k8|
               +-------------------------------+
               | v1| v2| v3| v4| v5| v6| v7| v8|
               +--------+----------------------+
                        |   *overflow  ||
                        +---------------+       +---+---+---+---+---+---+---+---+
                                        +------>+ 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 |
                                                +-------------------------------+
                                                | k1| k2| k3| k4| k5| k6| k7| k8|
                                                +-------------------------------+
                                                | v1| v2| v3| v4| v5| v6| v7| v8|
                                                +--------+----------------------+
                                                         |   *overflow  |
                                                         +--------------+

// 后面的代码中对于key是从dataOffset开始
// 对于value的寻址是从dataOffset + keysize * bucketCnt开始
```

> 代码中有些常用的位操作，为了便于后面的理解这里进行一些说明
```
// 2^b
func bucketShift(b uint8) uintptr {
	......
	return uintptr(1) << b
}
// 2^b - 1 将低b位置为1，方便hash计算的时候取低b位计算属于哪个桶
func bucketMask(b uint8) uintptr {
	return bucketShift(b) - 1
}
// 取高8位作为快速比对的依据，存在bmap的tophash中
// 这里对于top值做了minTopHash的偏移以保留几个标志位
func tophash(hash uintptr) uint8 {
	top := uint8(hash >> (sys.PtrSize*8 - 8))
	if top < minTopHash {
		top += minTopHash
	}
	return top
}
// 判断bucket是否迁移完成
func evacuated(b *bmap) bool {
	h := b.tophash[0]
	return h > emptyOne && h < minTopHash
}
// 计算overflow指针的位置
func (b *bmap) overflow(t *maptype) *bmap {
	return *(**bmap)(add(unsafe.Pointer(b), uintptr(t.bucketsize)-sys.PtrSize))
}
// 设置overflow指针
func (b *bmap) setoverflow(t *maptype, ovf *bmap) {
	*(**bmap)(add(unsafe.Pointer(b), uintptr(t.bucketsize)-sys.PtrSize)) = ovf
}
// 计算key的位置
func (b *bmap) keys() unsafe.Pointer {
	return add(unsafe.Pointer(b), dataOffset)
}
```

> 对于overflow当前bucket满了，overflow的bucket的操作和统计
```
// 当buckets数量少时，noverflow是个精确值
// 反之，过大时，是计算的近似值
// noverflow的关键是触发same size grow的关键指标，这部分逻辑处于tooManyOverflowBuckets中
func (h *hmap) incrnoverflow() {
	if h.B < 16 {
		h.noverflow++
		return
	}
	// Increment with probability 1/(1<<(h.B-15)).
	// When we reach 1<<15 - 1, we will have approximately
	// as many overflow buckets as buckets.
	mask := uint32(1)<<(h.B-15) - 1
	// Example: if h.B == 18, then mask == 7,
	// and fastrand & 7 == 0 with probability 1/8.
	if fastrand()&mask == 0 {
		h.noverflow++
	}
}

// new新的overflow
func (h *hmap) newoverflow(t *maptype, b *bmap) *bmap {
	var ovf *bmap
	if h.extra != nil && h.extra.nextOverflow != nil {
		// 提前预分配了冗余的buckets
		ovf = h.extra.nextOverflow
		if ovf.overflow(t) == nil {
			// 冗余的指针进一位
			h.extra.nextOverflow = (*bmap)(add(unsafe.Pointer(ovf), uintptr(t.bucketsize)))
		} else {
			// 预分配被耗尽
			ovf.setoverflow(t, nil)
			h.extra.nextOverflow = nil
		}
	} else {
		ovf = (*bmap)(newobject(t.bucket))
	}
	h.incrnoverflow()
	// 是非指针类型
	// 因为map的type被标志为kindNoPointers
	// 所以为了避免overflow被gc，需要单独hold这些指针
	/*
		In that code t is *maptype, which is to say it is a pointer to the 
		type descriptor for the map, essentially the same value you would get 
		from calling reflect.TypeOf on the map value.  t.bucket is a pointer 
		to the type descriptor for the type of the buckets that the map uses. 
		This type is created by the compiler based on the key and value types 
		of the map.  If the kindNoPointers bit is set in t.bucket.kind, then 
		the bucket type does not contain any pointers.  With the current 
		implementation, this will be true if the key and value types do not 
		themselves contain any pointers and both types are less than 128 
		bytes.  Whether the bucket type contains any pointers is interesting 
		because the garbage collector never has to look at buckets that 
		contain no pointers.  The current map implementation goes to some 
		effort to preserve that property.  See the comment in the mapextra 
		type.
	*/
	if t.bucket.kind&kindNoPointers != 0 {
		h.createOverflow()
		*h.extra.overflow = append(*h.extra.overflow, ovf)
	}
	b.setoverflow(t, ovf)
	return ovf
}
// 创建overflow
func (h *hmap) createOverflow() {
	if h.extra == nil {
		h.extra = new(mapextra)
	}
	if h.extra.overflow == nil {
		h.extra.overflow = new([]*bmap)
	}
}
```

## 创建
map的创建函数有两个关键函数: makemap & makeBucketArray
> map初始化过程
```
func makemap(t *maptype, hint int, h *hmap) *hmap {
	......

	// 基本的初始化逻辑，初始化结构体和hash种子
	if h == nil {
		h = new(hmap)
	}
	h.hash0 = fastrand()

	// 根据预分配的hint的大小，不停的查找合适的B值
	B := uint8(0)
	for overLoadFactor(hint, B) {
		B++
	}
	h.B = B

	// If hint is large zeroing this memory could take a while.
	// 如果没有预分配的要求，则会做在mapassign中做lazy init
	// 分配buckets内存，详情见下面的maketBucketArray
	if h.B != 0 {
		var nextOverflow *bmap
		h.buckets, nextOverflow = makeBucketArray(t, h.B, nil)
		if nextOverflow != nil {
			h.extra = new(mapextra)
			h.extra.nextOverflow = nextOverflow
		}
	}

	return h
}
```
> map的内存初始化过程
```
func makeBucketArray(t *maptype, b uint8, dirtyalloc unsafe.Pointer) (buckets unsafe.Pointer, nextOverflow *bmap) {
	// 2^B个bucket
	base := bucketShift(b)
	nbuckets := base

	// 如果超过8个，则要做预分配
	if b >= 4 {
		// 按照预估的逻辑增加对应的bucket
		nbuckets += bucketShift(b - 4)
		sz := t.bucket.size * nbuckets
		up := roundupsize(sz)
		if up != sz {
			nbuckets = up / t.bucket.size
		}
	}

	// 这里针对已有的分配进行内存清理工作
	// todo: 内存清理的细节还不太理解
	if dirtyalloc == nil {
		buckets = newarray(t.bucket, int(nbuckets))
	} else {
		// dirtyalloc was previously generated by
		// the above newarray(t.bucket, int(nbuckets))
		// but may not be empty.
		buckets = dirtyalloc
		size := t.bucket.size * nbuckets
		if t.bucket.kind&kindNoPointers == 0 {
			memclrHasPointers(buckets, size)
		} else {
			memclrNoHeapPointers(buckets, size)
		}
	}

	// 如果做了预分配
	if base != nbuckets {
		// 预分配的bucket的开始地址
		nextOverflow = (*bmap)(add(buckets, base*uintptr(t.bucketsize)))
		// 最后一个bucket的overflow要做个安全保护，指向buckets
		last := (*bmap)(add(buckets, (nbuckets-1)*uintptr(t.bucketsize)))
		last.setoverflow(t, (*bmap)(buckets))
	}
	return buckets, nextOverflow
}
```
## 访问&赋值
### 访问
> mapacccess系列函数返回h[key]的指针，即使元素不存在也不会返回nil,而是返回zero object.
> 注意: 返回的指针会导致map一直存活，所以不要保存太久导致影响map的内存回收
```
func mapaccess1(t *maptype, h *hmap, key unsafe.Pointer) unsafe.Pointer {
	......

	// 写状态标志位检查
	if h.flags&hashWriting != 0 {
		throw("concurrent map read and map write")
	}

	// hash以及通过hash的位操作定位桶的位置
	alg := t.key.alg
	hash := alg.hash(key, uintptr(h.hash0))
	m := bucketMask(h.B)
	b := (*bmap)(add(h.buckets, (hash&m)*uintptr(t.bucketsize)))

	// 如果oldbuckets不为空
	if c := h.oldbuckets; c != nil {
		// 如果是增量扩容，则mask要右移一位，因为只有老的只有当前一半的大小
		if !h.sameSizeGrow() {
			// There used to be half as many buckets; mask down one more power of two.
			m >>= 1
		}
		oldb := (*bmap)(add(c, (hash&m)*uintptr(t.bucketsize)))
		// 如果还没有迁移完成，则从老的里面查
		if !evacuated(oldb) {
			b = oldb
		}
	}
	top := tophash(hash)
bucketloop:
	// 这里参照单个桶的图示结构就是遍历bucket的overflow链表
	// 结合文章开头的基础描述很容易理解
	for ; b != nil; b = b.overflow(t) {
		for i := uintptr(0); i < bucketCnt; i++ {
			if b.tophash[i] != top {
				if b.tophash[i] == emptyRest {
					break bucketloop
				}
				continue
			}
			k := add(unsafe.Pointer(b), dataOffset+i*uintptr(t.keysize))
			if t.indirectkey() {
				k = *((*unsafe.Pointer)(k))
			}
			if alg.equal(key, k) {
				v := add(unsafe.Pointer(b), dataOffset+bucketCnt*uintptr(t.keysize)+i*uintptr(t.valuesize))
				if t.indirectvalue() {
					v = *((*unsafe.Pointer)(v))
				}
				return v
			}
		}
	}
	// 如果没有找到，则返回零值对象
	return unsafe.Pointer(&zeroVal[0])
}
```
> 赋值的过程和mapaccess类似区别在于会为不存在的key分配slot
```
func mapassign(t *maptype, h *hmap, key unsafe.Pointer) unsafe.Pointer {
	......

	alg := t.key.alg
	hash := alg.hash(key, uintptr(h.hash0))

	// 在hash之后做标志位设定是因为hash函数可能会panic
	// 为了防止panic后标志位无法释放
	h.flags ^= hashWriting

	// makemap中提到的延迟初始化
	if h.buckets == nil {
		h.buckets = newobject(t.bucket) // newarray(t.bucket, 1)
	}

again:
	bucket := hash & bucketMask(h.B)
	// 如果当前在迁移中，则迁移当前bucket
	if h.growing() {
		growWork(t, h, bucket)
	}
	b := (*bmap)(unsafe.Pointer(uintptr(h.buckets) + bucket*uintptr(t.bucketsize)))
	top := tophash(hash)

	var inserti *uint8
	var insertk unsafe.Pointer
	var val unsafe.Pointer
bucketloop:
	for {
		for i := uintptr(0); i < bucketCnt; i++ {
			if b.tophash[i] != top {
				if isEmpty(b.tophash[i]) && inserti == nil {
					inserti = &b.tophash[i]
					insertk = add(unsafe.Pointer(b), dataOffset+i*uintptr(t.keysize))
					val = add(unsafe.Pointer(b), dataOffset+bucketCnt*uintptr(t.keysize)+i*uintptr(t.valuesize))
				}
				// 当前bucket剩下的都为空
				if b.tophash[i] == emptyRest {
					// 已经找到插入位置，跳出大循环
					break bucketloop
				}
				continue
			}
			k := add(unsafe.Pointer(b), dataOffset+i*uintptr(t.keysize))
			if t.indirectkey() {
				k = *((*unsafe.Pointer)(k))
			}
			// 前8位相当的情况下还要做一次key hash比对
			if !alg.equal(key, k) {
				continue
			}
			// 已存在，则更新
			if t.needkeyupdate() {
				typedmemmove(t.key, k, key)
			}
			val = add(unsafe.Pointer(b), dataOffset+bucketCnt*uintptr(t.keysize)+i*uintptr(t.valuesize))
			goto done
		}

		// 没有找到，则到溢出桶中找
		ovf := b.overflow(t)
		if ovf == nil {
			// 溢出桶也没了，则跳出，进入allocate流程
			break
		}
		b = ovf
	}

	// 这里会触发扩容操作
	if !h.growing() && (overLoadFactor(h.count+1, h.B) || tooManyOverflowBuckets(h.noverflow, h.B)) {
		hashGrow(t, h)
		goto again // Growing the table invalidates everything, so try again
	}

	// 分配新的溢出桶
	if inserti == nil {
		// all current buckets are full, allocate a new one.
		newb := h.newoverflow(t, b)
		inserti = &newb.tophash[0]
		insertk = add(unsafe.Pointer(newb), dataOffset)
		val = add(insertk, bucketCnt*uintptr(t.keysize))
	}

	// 对k v 进行赋值，并且元素数量计数+1
	if t.indirectkey() {
		kmem := newobject(t.key)
		*(*unsafe.Pointer)(insertk) = kmem
		insertk = kmem
	}
	if t.indirectvalue() {
		vmem := newobject(t.elem)
		*(*unsafe.Pointer)(val) = vmem
	}
	typedmemmove(t.key, insertk, key)
	*inserti = top
	h.count++

	// 清理标志位 & 赋值
done:
	if h.flags&hashWriting == 0 {
		throw("concurrent map writes")
	}
	h.flags &^= hashWriting
	if t.indirectvalue() {
		val = *((*unsafe.Pointer)(val))
	}
	return val
}
```
## 扩容
> 上面的赋值过程中，有两个触发扩容操作
> 	1. 赋值过程中，如果发现正在扩容，则进行两次扩容
>	2. 没有找到新建元素的时候，

```
// 扩容1
func growWork(t *maptype, h *hmap, bucket uintptr) {
	// 确认我们要查找的bucket已经迁移完成
	evacuate(t, h, bucket&h.oldbucketmask())

	// 多触发一次迁移，渐进式扩容
	if h.growing() {
		evacuate(t, h, h.nevacuate)
	}
}
```

```
// 扩容2
hashGrow(t, h)

func hashGrow(t *maptype, h *hmap) {
	// 根据当前数据，判断是增量扩容还是等量扩容
	bigger := uint8(1)
	if !overLoadFactor(h.count+1, h.B) {
		bigger = 0
		h.flags |= sameSizeGrow
	}
	oldbuckets := h.buckets
	newbuckets, nextOverflow := makeBucketArray(t, h.B+bigger, nil)

	// &^ 按位置 0运算符 
	// x = 01010011
	// y = 01010100
	// z = x &^ y = 00000011
	// 如果ybit位为1，那么结果z对应bit位就为0，否则z对应bit位就和x对应bit位的值相同
	// 先把 h.flags 中 iterator 和 oldIterator 对应位清 0，然后如果发现 iterator 位为 1，那就把它转接到 oldIterator 位，使得 oldIterator 标志位变成 1。潜台词就是：buckets 现在挂到了 oldBuckets 名下了，对应的标志位也转接过去吧
	flags := h.flags &^ (iterator | oldIterator)
	if h.flags&iterator != 0 {
		flags |= oldIterator
	}
	// 重新设置B值，初始化相关变量
	h.B += bigger
	h.flags = flags
	h.oldbuckets = oldbuckets
	h.buckets = newbuckets
	h.nevacuate = 0
	h.noverflow = 0

	if h.extra != nil && h.extra.overflow != nil {
		// 当前的溢出切换为老的溢出
		if h.extra.oldoverflow != nil {
			throw("oldoverflow is not nil")
		}
		h.extra.oldoverflow = h.extra.overflow
		h.extra.overflow = nil
	}
	// 重新设置新的溢出页
	if nextOverflow != nil {
		if h.extra == nil {
			h.extra = new(mapextra)
		}
		h.extra.nextOverflow = nextOverflow
	}

	// 这里只是触发扩容，真正的扩容是在growWork()和evacuate()里面进行的，并且是渐进式的
}
```

> evacuate大体流程
```
// 在老的buckets中的位置
b := (*bmap)(add(h.oldbuckets, oldbucket*uintptr(t.bucketsize)))
// 老的buckets的数量
newbit := h.noldbuckets()
if !evacuated(b) {
	...
}
// 如果已经迁移过了，则判断是否正好是当前进度
if oldbucket == h.nevacuate {
	// 更新当前进度
	advanceEvacuationMark(h, t, newbit)
}

// 更新进度 h.nevacuate表示当前位置之前的buckets都完成了迁移
func advanceEvacuationMark(h *hmap, t *maptype, newbit uintptr) {
	h.nevacuate++
	// Experiments suggest that 1024 is overkill by at least an order of magnitude.
	// Put it in there as a safeguard anyway, to ensure O(1) behavior.
	stop := h.nevacuate + 1024
	if stop > newbit {
		stop = newbit
	}
	// 查找往后第一个未迁移的bucket
	for h.nevacuate != stop && bucketEvacuated(t, h, h.nevacuate) {
		h.nevacuate++
	}

	if h.nevacuate == newbit { // 已迁移数量等于老的buckets数量
		// 迁移完成
		h.oldbuckets = nil
		// [1] 表示 old overflow bucket
		if h.extra != nil {
			h.extra.oldoverflow = nil
		}
		// 清除正在扩容的标志位
		h.flags &^= sameSizeGrow
	}
}
```
> evacuate 详细扩容操作
```
type evacDst struct {
	b *bmap          // 当前buckets指针
	i int            // b的kv的索引
	k unsafe.Pointer // key的指针
	v unsafe.Pointer // value的指针
}
// 增量扩容的情况下，x & y容量两倍后的上下半区
var xy [2]evacDst
x := &xy[0]
x.b = (*bmap)(add(h.buckets, oldbucket*uintptr(t.bucketsize)))
x.k = add(unsafe.Pointer(x.b), dataOffset)
x.v = add(x.k, bucketCnt*uintptr(t.keysize))

if !h.sameSizeGrow() {
	// 如果是增量扩容，则用y保持下半区指针
	// 避免gc发现bad pointer
	y := &xy[1]
	y.b = (*bmap)(add(h.buckets, (oldbucket+newbit)*uintptr(t.bucketsize)))
	y.k = add(unsafe.Pointer(y.b), dataOffset)
	y.v = add(y.k, bucketCnt*uintptr(t.keysize))
}

for ; b != nil; b = b.overflow(t) {
	k := add(unsafe.Pointer(b), dataOffset)
	v := add(k, bucketCnt*uintptr(t.keysize))
	for i := 0; i < bucketCnt; i, k, v = i+1, add(k, uintptr(t.keysize)), add(v, uintptr(t.valuesize)) {
		top := b.tophash[i]
		// 空的cell处理
		if isEmpty(top) {
			b.tophash[i] = evacuatedEmpty
			continue
		}
		if top < minTopHash {
			throw("bad map state")
		}
		k2 := k
		if t.indirectkey() {
			k2 = *((*unsafe.Pointer)(k2))
		}
		var useY uint8
		// 如果是等量扩容，因为掩码多个一位，所以要重新判断处于x区还是y区
		if !h.sameSizeGrow() {
			// Compute hash to make our evacuation decision (whether we need
			// to send this key/value to bucket x or bucket y).
			hash := t.key.alg.hash(k2, uintptr(h.hash0))
			if h.flags&iterator != 0 && !t.reflexivekey() && !t.key.alg.equal(k2, k2) {
				/*
				本段参照: https://www.cnblogs.com/qcrao-2018/archive/2019/05/22/10903807.html
				有一个特殊情况是：有一种 key，每次对它计算 hash，得到的结果都不一样。这个 key 就是 math.NaN() 的结果，它的含义是 not a number，类型是 float64。当它作为 map 的 key，在搬迁的时候，会遇到一个问题：再次计算它的哈希值和它当初插入 map 时的计算出来的哈希值不一样！

				你可能想到了，这样带来的一个后果是，这个 key 是永远不会被 Get 操作获取的！当我使用 m[math.NaN()] 语句的时候，是查不出来结果的。这个 key 只有在遍历整个 map 的时候，才有机会现身。所以，可以向一个 map 插入任意数量的 math.NaN() 作为 key。

				当搬迁碰到 math.NaN() 的 key 时，只通过 tophash 的最低位决定分配到 X part 还是 Y part（如果扩容后是原来 buckets 数量的 2 倍）。如果 tophash 的最低位是 0 ，分配到 X part；如果是 1 ，则分配到 Y part。
				*/
				useY = top & 1
				top = tophash(hash)
			} else {
				if hash&newbit != 0 {
					useY = 1
				}
			}
		}

		......

		// 根绝x & y来选择目标位置
		b.tophash[i] = evacuatedX + useY // evacuatedX + 1 == evacuatedY
		dst := &xy[useY]                 // evacuation destination

		if dst.i == bucketCnt {
			dst.b = h.newoverflow(t, dst.b)
			dst.i = 0
			dst.k = add(unsafe.Pointer(dst.b), dataOffset)
			dst.v = add(dst.k, bucketCnt*uintptr(t.keysize))
		}
		dst.b.tophash[dst.i&(bucketCnt-1)] = top // mask dst.i as an optimization, to avoid a bounds check

		// 对k&v进行迁移赋值
		if t.indirectkey() {
			*(*unsafe.Pointer)(dst.k) = k2 // copy pointer
		} else {
			typedmemmove(t.key, dst.k, k) // copy value
		}
		if t.indirectvalue() {
			*(*unsafe.Pointer)(dst.v) = *(*unsafe.Pointer)(v)
		} else {
			typedmemmove(t.elem, dst.v, v)
		}
		dst.i++
		// These updates might push these pointers past the end of the
		// key or value arrays.  That's ok, as we have the overflow pointer
		// at the end of the bucket to protect against pointing past the
		// end of the bucket.
		dst.k = add(dst.k, uintptr(t.keysize))
		dst.v = add(dst.v, uintptr(t.valuesize))
	}
}

......

```

## 删除
删除的操作代码和access比较类似，只是增加了key value的删除以及tophash的标志位设定(emptyOne & emptyRest)

## 遍历
```
type hiter struct {
	key         unsafe.Pointer
	value       unsafe.Pointer
	t           *maptype
	h           *hmap
	buckets     unsafe.Pointer 	// hash_iter初始化时的buckets指针
	bptr        *bmap          	// 当前的桶
	overflow    *[]*bmap       	// keeps overflow buckets of hmap.buckets alive
	oldoverflow *[]*bmap       	// keeps overflow buckets of hmap.oldbuckets alive
	startBucket uintptr        	// 遍历起点的bucket
	offset      uint8          	// 遍历过程中bucket内部cell的偏移值
	wrapped     bool           	// 是否从头遍历了
	B           uint8			// B
	i           uint8			// cell索引
	bucket      uintptr			// 当前的bucket
	checkBucket uintptr			// 因为扩容需要检查的bucket
}
```

遍历的关键过程在mapiternext里，这里针对这个函数进行注解
```
func mapiternext(it *hiter) {
	......

	t := it.t
	bucket := it.bucket
	b := it.bptr
	i := it.i
	checkBucket := it.checkBucket
	alg := t.key.alg

next:
	if b == nil {
		// 因为是随机位置的bucket开始的，所以这里判断是否已经循环了一遍
		if bucket == it.startBucket && it.wrapped {
			// end of iteration
			it.key = nil
			it.value = nil
			return
		}
		// 因为扩容是渐进式的，所以很有可能在扩容的过程中遍历
		if h.growing() && it.B == h.B {
			// 如果老的bucket没有迁移完成，则去遍历老的bucket，但是因为老的bucket的key有可能
			// 会到新区的不同bucket，所以只会遍历会合并到当前bucket的cell
			oldbucket := bucket & it.h.oldbucketmask()
			b = (*bmap)(add(h.oldbuckets, oldbucket*uintptr(t.bucketsize)))
			if !evacuated(b) {
				checkBucket = bucket
			} else {
				b = (*bmap)(add(it.buckets, bucket*uintptr(t.bucketsize)))
				checkBucket = noCheck
			}
		} else {
			b = (*bmap)(add(it.buckets, bucket*uintptr(t.bucketsize)))
			checkBucket = noCheck
		}
		bucket++
		if bucket == bucketShift(it.B) {
			bucket = 0
			it.wrapped = true
		}
		i = 0
	}
	// 遍历bucket的cell
	for ; i < bucketCnt; i++ {
		offi := (i + it.offset) & (bucketCnt - 1)
		if isEmpty(b.tophash[offi]) || b.tophash[offi] == evacuatedEmpty {
			continue
		}
		k := add(unsafe.Pointer(b), dataOffset+uintptr(offi)*uintptr(t.keysize))
		if t.indirectkey() {
			k = *((*unsafe.Pointer)(k))
		}
		v := add(unsafe.Pointer(b), dataOffset+bucketCnt*uintptr(t.keysize)+uintptr(offi)*uintptr(t.valuesize))
		if checkBucket != noCheck && !h.sameSizeGrow() {
			// 老bucket没有迁移完且是增量扩容
			// 会跳过那些分配到新的bucket的key
			if t.reflexivekey() || alg.equal(k, k) {
				// If the item in the oldbucket is not destined for
				// the current new bucket in the iteration, skip it.
				hash := alg.hash(k, uintptr(h.hash0))
				if hash&bucketMask(it.B) != checkBucket {
					continue
				}
			} else {
				// NaN的处理是取tophash的低1位来判断
				if checkBucket>>(it.B-1) != uintptr(b.tophash[offi]&1) {
					continue
				}
			}
		}
		if (b.tophash[offi] != evacuatedX && b.tophash[offi] != evacuatedY) ||
			!(t.reflexivekey() || alg.equal(k, k)) {
			// This is the golden data, we can return it.
			// OR
			// key!=key, so the entry can't be deleted or updated, so we can just return it.
			// That's lucky for us because when key!=key we can't look it up successfully.
			it.key = k
			if t.indirectvalue() {
				v = *((*unsafe.Pointer)(v))
			}
			it.value = v
		} else {
            // NOTE: 这里要重新查找的原因是，有可能在range遍历的循环中删除元素
			// 参照: https://golang.org/ref/spec#For_statements
			// The iteration order over maps is not specified and is not guaranteed to be the same from one iteration to the next.
			// If a map entry that has not yet been reached is removed during iteration, the corresponding iteration value will not be produced.
			// If a map entry is created during iteration, that entry may be produced during the iteration or may be skipped.
			// The choice may vary for each entry created and from one iteration to the next. If the map is nil, the number of iterations is 0.
			rk, rv := mapaccessK(t, h, k)
			if rk == nil {
				continue // key has been deleted
			}
			it.key = rk
			it.value = rv
		}
		it.bucket = bucket
		if it.bptr != b { // avoid unnecessary write barrier; see issue 14921
			it.bptr = b
		}
		it.i = i + 1
		it.checkBucket = checkBucket
		return
	}
	// 遍历溢出bucket
	b = b.overflow(t)
	i = 0
	goto next
}

// 图解
   +----------------+                                      +----------------+-------+-------+
   |       ||       |                                      |       ||       |       |       |
   |   0   ||   1   |                                      |   0   ||   1   |   3   |   4   |
   |       ||       |                                      |       ||       |       |       |
   +-------------+--+                                      +-------------+--+-------+---+---+
                 |                                                       |              |
                 |                                                       |              |
                 |                                                       |              |
                 v                                                       v              v
+---+---+---+---++--+---+---+---+        +---+---+---+---+---+---+---+---+        +---+-+-+---+---+---+---+---+---+
|   |   |   | 4 |   | 6 |   |   |        | 1 |   |   | 4 |   | 6 |   |   |        |   |   |   |   |   |   |   | 8 |
+-------------------------------+        +-------------------------------+        +-------------------------------+
|   |   |   | k4|   | k6|   |   |        | k1|   |   | k4|   | k6|   |   |        |   |   |   |   |   |   |   | k8|
+-------------------------------+        +-------------------------------+        +-------------------------------+
|   |   |   | v4|   | v6|   |   |        | v1|   |   | v4|   | v6|   |   |        |   |   |   |   |   |   |   | v8|
+---+---++--+-------+-------+---+        +-------++--+-------+-------+---+        +---+---++--+---+---+-------+---+
         |   *overflow  ||                        |   *overflow  ||                        |   *overflow  |
         +-------+-------+                        +---------------+                        +--------------+
                 |
                 |
                 v
+---+---+---+---++--+---+---+---+
| 1 |   |   |   |   |   |   | 8 |
+-------------------------------+
| k1|   |   |   |   |   |   | k8|
+-------------------------------+
| v1|   |   |   |   |   |   | v8|
+-------++--+---+---+-------+---+
         |   *overflow  |
         +--------------+


// 对于同一个bucket里的cell来说，有可能迁移后不在当前bucket，位于Y的上半区了，例如k8
```
