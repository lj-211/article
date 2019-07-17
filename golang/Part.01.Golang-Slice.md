# go数据结构 - slice
```
type slice struct {
	array unsafe.Pointer	// 数组指针
	len   int				// 长度
	cap   int				// 容量
}
```
## 创建
```
func makeslice(et *_type, len, cap int) unsafe.Pointer {
	mem, overflow := math.MulUintptr(et.size, uintptr(cap))
	if overflow || mem > maxAlloc || len < 0 || len > cap {
		// 为了让错误信息更直观
		// 再检查一次len
		mem, overflow := math.MulUintptr(et.size, uintptr(len))
		if overflow || mem > maxAlloc || len < 0 {
			// panic 长度超过范围
			panicmakeslicelen()
		}
		// panic 容量超过范围
		panicmakeslicecap()
	}

	// 分配空间
	return mallocgc(mem, et, true)
}
```

## 扩容
```
// append的时候检查如果超过容量则调用growslice扩容
func growslice(et *_type, old slice, cap int) slice {
	
	......

	// 扩容策略
	newcap := old.cap
	doublecap := newcap + newcap
	// 如果容量是老容量的两倍，则直接使用参数容量
	if cap > doublecap {
		newcap = cap
	} else {
		// 老的容量大小小于1024，则容量扩大两倍
		if old.len < 1024 {
			newcap = doublecap
		} else {
			// newcap > 0 的判断是为了防止溢出
			// 以1.25为放大因子，扩容到第一个大于cap的数值
			for 0 < newcap && newcap < cap {
				newcap += newcap / 4
			}
			// 如果溢出了，则使用参数容量
			if newcap <= 0 {
				newcap = cap
			}
		}
	}

	var overflow bool
	var lenmem, newlenmem, capmem uintptr
	// todo: newlenmem 变量的意义是什么？
	switch {
	case et.size == 1:	// 如果et.size是1，则不需要做任何除法乘法
		lenmem = uintptr(old.len)
		newlenmem = uintptr(cap)
		capmem = roundupsize(uintptr(newcap))
		overflow = uintptr(newcap) > maxAlloc
		newcap = int(capmem)
	case et.size == sys.PtrSize:	// et.size是指针大小则使用sys.PtrSize,编译器会优化乘除操作
		lenmem = uintptr(old.len) * sys.PtrSize
		newlenmem = uintptr(cap) * sys.PtrSize
		capmem = roundupsize(uintptr(newcap) * sys.PtrSize)
		overflow = uintptr(newcap) > maxAlloc/sys.PtrSize
		newcap = int(capmem / sys.PtrSize)
	case isPowerOfTwo(et.size):		// et.size是2的平方，则直接用位操作
		var shift uintptr
		if sys.PtrSize == 8 {
			// Mask shift for better code generation.
			shift = uintptr(sys.Ctz64(uint64(et.size))) & 63
		} else {
			shift = uintptr(sys.Ctz32(uint32(et.size))) & 31
		}
		lenmem = uintptr(old.len) << shift
		newlenmem = uintptr(cap) << shift
		capmem = roundupsize(uintptr(newcap) << shift)
		overflow = uintptr(newcap) > (maxAlloc >> shift)
		newcap = int(capmem >> shift)
	default: // 非特殊大小则自己做乘除法
		lenmem = uintptr(old.len) * et.size
		newlenmem = uintptr(cap) * et.size
		capmem, overflow = math.MulUintptr(et.size, uintptr(newcap))
		capmem = roundupsize(capmem)
		newcap = int(capmem / et.size)
	}

	// 检查是否溢出
	if overflow || capmem > maxAlloc {
		panic(errorString("growslice: cap out of range"))
	}

	// 分配和清理内存
	var p unsafe.Pointer
	if et.kind&kindNoPointers != 0 {
		p = mallocgc(capmem, nil, false)
		// 清理新内存
		memclrNoHeapPointers(add(p, newlenmem), capmem-newlenmem)
	} else {
		// Note: can't use rawmem (which avoids zeroing of memory), because then GC can scan uninitialized memory.
		p = mallocgc(capmem, et, true)
		if writeBarrier.enabled {
			bulkBarrierPreWriteSrcOnly(uintptr(p), uintptr(old.array), lenmem)
		}
	}
	// 拷贝数据
	memmove(p, old.array, lenmem)

	return slice{p, old.len, newcap}
}
```
## tips
### Nil slice & Empty Slice的区别

```
     Nil Slice                    Empty Slice


+-------+----------+         +-------+----------+
| array |   nil    |         | array |  []int{} |
+------------------+         +------------------+
|  len  |    0     |         |  len  |    0     |
+------------------+         +------------------+
|  cap  |    0     |         |  cap  |    0     |
+-------+----------+         +-------+----------+
```
### 小心陷阱
```
func main() {
	array := []int{1, 2, 3, 4, 5}
	s1 := array[0:2:3]
	fmt.Println(unsafe.Pointer(&s1[0])
	fmt.Println(array, len(s1), cap(s1))
	fmt.Println("------------------")
	fmt.Println(unsafe.Pointer(&s1[0])
	fmt.Println(s1, len(s1), cap(s1))
	fmt.Println("------------------")
	s1 = append(s1, 99)
	fmt.Println(unsafe.Pointer(&s1[0])
	fmt.Println(s1, len(s1), cap(s1))
	fmt.Println("------------------")
	s1 = append(s1, 98)
	fmt.Println(unsafe.Pointer(&s1[0])
	fmt.Println(s1, len(s1), cap(s1))
}

0x450000  
[1 2 3 4 5] 2 3
------------------
0x450000  
[1 2] 2 3			-----------> 老的array的cap被修改
------------------
0x450000  
[1 2 99] 3 3
------------------
0x450040  			-----------> 触发扩容，地址发生变化
[1 2 99 98] 4 8
```
