# 工具类中的数据结构之SRefSet

## 图解
![avatar](https://guttural-hedgehog-61d.notion.site/image/https%3A%2F%2Fs3-us-west-2.amazonaws.com%2Fsecure.notion-static.com%2F3f9ef496-ad12-450d-b8bb-72a699d9031f%2FTDengine源码分析_-_utils_tref_-_1.png?table=block&id=d96bba6e-d52f-4f83-b517-6ebf40e637a9&spaceId=70e3249c-a548-4fcf-82aa-ca826c601db0&width=2000&userId=&cache=v2)

## 核心源码注释
### 数据结构
```c
// 双向链表实现
typedef struct SRefNode {
  struct SRefNode  *prev;  // 前节点
  struct SRefNode  *next;  // 后节点
  void             *p;     // 受保护的资源
  int64_t           rid;   // 引用id
  int32_t           count; // 引用技术
  int               removed; // 1: 删除
} SRefNode;

typedef struct {
  SRefNode **nodeList; // 节点数组
  int        state;    // 0: empty, 1: active;  2: deleted
  int        rsetId;   // 全局唯一的资源组id
  int64_t    rid;      // increase by one for each new reference
  int        max;      // mod
  int32_t    count;    // 集合中的引用节点
  int64_t   *lockedBy;
  void     (*fp)(void *);
} SRefSet;

// 全局ResSet列表
static SRefSet         tsRefSetList[TSDB_REF_OBJECTS];
// once init
static pthread_once_t  tsRefModuleInit = PTHREAD_ONCE_INIT;
// 全局RetSet互斥锁
static pthread_mutex_t tsRefMutex;
// 全局RefSet的数量
static int             tsRefSetNum = 0;
// todo: ?
static int             tsNextId = 0;

// 初始化全局tsRefMutex
static void taosInitRefModule(void);

// 锁定RefSet
// todo: lockedBy是干嘛的？
static void taosLockList(int64_t *lockedBy);
static void taosLockList(int64_t *lockedBy) {
  int64_t tid = taosGetSelfPthreadId();
  int     i = 0;
  while (atomic_val_compare_exchange_64(lockedBy, 0, tid) != 0) {
    if (++i % 100 == 0) {
			// 让出cpu
      sched_yield();
    }
  }
}

// 解锁RefSet
static void taosUnlockList(int64_t *lockedBy);
static void taosUnlockList(int64_t *lockedBy) {
  int64_t tid = taosGetSelfPthreadId();
  if (atomic_val_compare_exchange_64(lockedBy, tid, 0) != tid) {
    assert(false);
  }
}

// 增加RefSet引用计数
static void taosIncRsetCount(SRefSet *pSet);
static void taosIncRsetCount(SRefSet *pSet) {
  atomic_add_fetch_32(&pSet->count, 1);
  // uTrace("rsetId:%d inc count:%d", pSet->rsetId, count);
}

// 减少RefSet引用计数
static void taosDecRsetCount(SRefSet *pSet);
static void taosDecRsetCount(SRefSet *pSet) {
  int32_t count = atomic_sub_fetch_32(&pSet->count, 1);
  // uTrace("rsetId:%d dec count:%d", pSet->rsetId, count);

  if (count > 0) return;

	// 如果等于0，则开始清理工作
  pthread_mutex_lock(&tsRefMutex);

	// todo: 为什么要维护状态？会带来数据一致性问题
  if (pSet->state != TSDB_REF_STATE_EMPTY) {
		// 重置RefSet状态数据
    pSet->state = TSDB_REF_STATE_EMPTY;
    pSet->max = 0;
    pSet->fp = NULL;

    tfree(pSet->nodeList);
    tfree(pSet->lockedBy);

		// 全局RefSet数量减1
    tsRefSetNum--;
    uTrace("rsetId:%d is cleaned, refSetNum:%d count:%d", pSet->rsetId, tsRefSetNum, pSet->count);
  }

  pthread_mutex_unlock(&tsRefMutex);
}

static int  taosDecRefCount(int rsetId, int64_t rid, int utl_remove);
static int taosDecRefCount(int rsetId, int64_t rid, int utl_remove) {
  int       hash;
  SRefSet  *pSet;
  SRefNode *pNode;
  int       released = 0;
  int       code = 0;

  if (rsetId < 0 || rsetId >= TSDB_REF_OBJECTS) {
    uTrace("rsetId:%d rid:%" PRId64 " failed to remove, rsetId not valid", rsetId, rid);
    terrno = TSDB_CODE_REF_INVALID_ID;
    return -1;
  }

  if (rid <= 0) {
    uTrace("rsetId:%d rid:%" PRId64 " failed to remove, rid not valid", rsetId, rid);
    terrno = TSDB_CODE_REF_NOT_EXIST;
    return -1;
  }

  pSet = tsRefSetList + rsetId;
  if (pSet->state == TSDB_REF_STATE_EMPTY) {
    uTrace("rsetId:%d rid:%" PRId64 " failed to remove, cleaned", rsetId, rid);
    terrno = TSDB_CODE_REF_ID_REMOVED;
    return -1;
  }

  hash = rid % pSet->max;
  taosLockList(pSet->lockedBy+hash);

  pNode = pSet->nodeList[hash];
  while (pNode) {
    if (pNode->rid == rid)
      break;

    pNode = pNode->next;
  }

  if (pNode) {
    pNode->count--;
    if (utl_remove) pNode->removed = 1;

    if (pNode->count <= 0) {
      if (pNode->prev) {
        pNode->prev->next = pNode->next;
      } else {
        pSet->nodeList[hash] = pNode->next;
      }

      if (pNode->next) {
        pNode->next->prev = pNode->prev;
      }
      released = 1;
    } else {
       uTrace("rsetId:%d p:%p rid:%" PRId64 " is released, count:%d", rsetId, pNode->p, rid, pNode->count);
    }
  } else {
    uTrace("rsetId:%d rid:%" PRId64 " is not there, failed to release/remove", rsetId, rid);
    terrno = TSDB_CODE_REF_NOT_EXIST;
    code = -1;
  }

  taosUnlockList(pSet->lockedBy+hash);

  if (released) {
    uTrace("rsetId:%d p:%p rid:%" PRId64 " is removed, count:%d, free mem: %p", rsetId, pNode->p, rid, pSet->count, pNode);
    (*pSet->fp)(pNode->p);
    free(pNode);

    taosDecRsetCount(pSet);
  }

  return code;
}
```

### 核心代码
```c
int taosOpenRef(int max, void (*fp)(void *))
{
  SRefNode **nodeList;
  SRefSet   *pSet;
  int64_t   *lockedBy;
  int        i, rsetId;

	// module初始化
  pthread_once(&tsRefModuleInit, taosInitRefModule);

	// 分配内存
  nodeList = calloc(sizeof(SRefNode *), (size_t)max);
  if (nodeList == NULL)  {
    terrno = TSDB_CODE_REF_NO_MEMORY;
    return -1;
  }

  lockedBy = calloc(sizeof(int64_t), (size_t)max);
  if (lockedBy == NULL) {
    free(nodeList);
    terrno = TSDB_CODE_REF_NO_MEMORY;
    return -1;
  }

  pthread_mutex_lock(&tsRefMutex);

	// 找到第一个空状态的RefSet
	// todo: 这里为什么废弃了索引为0的RefSet？
  for (i = 0; i < TSDB_REF_OBJECTS; ++i) {
    tsNextId = (tsNextId + 1) % TSDB_REF_OBJECTS;
    if (tsNextId == 0) tsNextId = 1;   // dont use 0 as rsetId
    if (tsRefSetList[tsNextId].state == TSDB_REF_STATE_EMPTY) break;
  }


  if (i < TSDB_REF_OBJECTS) {
    rsetId = tsNextId;
    pSet = tsRefSetList + rsetId;
    pSet->max = max;
    pSet->nodeList = nodeList;
    pSet->lockedBy = lockedBy;
    pSet->fp = fp;
    pSet->rid = 1;
    pSet->rsetId = rsetId;
    pSet->state = TSDB_REF_STATE_ACTIVE;
    taosIncRsetCount(pSet);

    tsRefSetNum++;
    uTrace("rsetId:%d is opened, max:%d, fp:%p refSetNum:%d", rsetId, max, fp, tsRefSetNum);
  } else {
    rsetId = TSDB_CODE_REF_FULL;
    free (nodeList);
    free (lockedBy);
    uTrace("run out of Ref ID, maximum:%d refSetNum:%d", TSDB_REF_OBJECTS, tsRefSetNum);
  }

  pthread_mutex_unlock(&tsRefMutex);

  return rsetId;
}

int taosCloseRef(int rsetId)
{
  SRefSet  *pSet;
  int       deleted = 0;

  if (rsetId < 0 || rsetId >= TSDB_REF_OBJECTS) {
    uTrace("rsetId:%d is invalid, out of range", rsetId);
    terrno = TSDB_CODE_REF_INVALID_ID;
    return -1;
  }

  pSet = tsRefSetList + rsetId;

  pthread_mutex_lock(&tsRefMutex);

  if (pSet->state == TSDB_REF_STATE_ACTIVE) {
    pSet->state = TSDB_REF_STATE_DELETED;
    deleted = 1;
    uTrace("rsetId:%d is closed, count:%d", rsetId, pSet->count);
  } else {
    uTrace("rsetId:%d is already closed, count:%d", rsetId, pSet->count);
  }

  pthread_mutex_unlock(&tsRefMutex);

  if (deleted) taosDecRsetCount(pSet);

  return 0;
}

// 迭代遍历ref
// if rid is 0, return the first p in hash list, otherwise, return the next after current rid
void *taosIterateRef(int rsetId, int64_t rid) {
  SRefNode *pNode = NULL;
  SRefSet  *pSet;

  if (rsetId < 0 || rsetId >= TSDB_REF_OBJECTS) {
    uTrace("rsetId:%d rid:%" PRId64 " failed to iterate, rsetId not valid", rsetId, rid);
    terrno = TSDB_CODE_REF_INVALID_ID;
    return NULL;
  }

  if (rid < 0) {
    uTrace("rsetId:%d rid:%" PRId64 " failed to iterate, rid not valid", rsetId, rid);
    terrno = TSDB_CODE_REF_NOT_EXIST;
    return NULL;
  }

  void *newP = NULL;
  pSet = tsRefSetList + rsetId;
  taosIncRsetCount(pSet);
  if (pSet->state != TSDB_REF_STATE_ACTIVE) {
    uTrace("rsetId:%d rid:%" PRId64 " failed to iterate, rset not active", rsetId, rid);
    terrno = TSDB_CODE_REF_ID_REMOVED;
    taosDecRsetCount(pSet);
    return NULL;
  }

  do {
    newP = NULL;
    int hash = 0;
    if (rid > 0) {
      hash = rid % pSet->max;
      taosLockList(pSet->lockedBy+hash);

      pNode = pSet->nodeList[hash];
      while (pNode) {
        if (pNode->rid == rid) break;
        pNode = pNode->next;
      }

      if (pNode == NULL) {
        uError("rsetId:%d rid:%" PRId64 " not there, quit", rsetId, rid);
        terrno = TSDB_CODE_REF_NOT_EXIST;
        taosUnlockList(pSet->lockedBy+hash);
        taosDecRsetCount(pSet);
        return NULL;
      }

      // rid is there
      pNode = pNode->next;
      // check first place
      while (pNode) {
        if (!pNode->removed) break;
        pNode = pNode->next;
      }
      if (pNode == NULL) {
        taosUnlockList(pSet->lockedBy+hash);
        hash++;
      }
    }

    if (pNode == NULL) {
      for (; hash < pSet->max; ++hash) {
        taosLockList(pSet->lockedBy+hash);
        pNode = pSet->nodeList[hash];
        if (pNode) {
          // check first place
          while (pNode) {
            if (!pNode->removed) break;
            pNode = pNode->next;
          }
          if (pNode) break;
        }
        taosUnlockList(pSet->lockedBy+hash);
      }
    }

    if (pNode) {
      pNode->count++;  // acquire it
      newP = pNode->p;
      taosUnlockList(pSet->lockedBy+hash);
      uTrace("rsetId:%d p:%p rid:%" PRId64 " is returned", rsetId, newP, rid);
    } else {
      uTrace("rsetId:%d the list is over", rsetId);
    }

    if (rid > 0) taosReleaseRef(rsetId, rid);  // release the current one
    if (pNode) rid = pNode->rid;
  } while (newP && pNode->removed);

  taosDecRsetCount(pSet);

  return newP;
}
```
