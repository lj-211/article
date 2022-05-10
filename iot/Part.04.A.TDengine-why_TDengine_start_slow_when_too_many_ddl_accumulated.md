# TDengine的mnode的启动逻辑

## 背景知识
这部分源码用到了两个基础工具类，请阅读前阅读以下两篇文章:
- [**TDengine SHashObj**](Part.04.A.TDengine-util_hashtable.md)
- [**TDengine SRefSet**](Part.04.A.TDengine-util_tref.md)

## mnode主要数据结构
<p align="center">
  <img width="1008" height = "700" src="../res/asc-img/%5BPart.04.A.TDengine-why_TDengine_start_slow_when_too_many_ddl_accumulated%5D%20P1%20-%20Data%20Structure.png" alt="data">
</p>
<p align="center">P1 - mnode data struture</p>

```c
typedef struct SSdbRow {
  ESdbOper   type;
  int32_t    processedCount;  // for sync fwd callback
  int32_t    code;            // for callback in sdb queue
  int32_t    rowSize;
  void *     rowData;
  void *     pObj;
  void *     pTable;
  SMnodeMsg *pMsg;
  int32_t  (*fpReq)(SMnodeMsg *pMsg);
  int32_t  (*fpRsp)(SMnodeMsg *pMsg, int32_t code);
  char       reserveForSync[24];
  SWalHead   pHead;
} SSdbRow;
```

```c
// mnode sdb插入行数据
int32_t sdbInsertRow(SSdbRow *pRow) {
  SSdbTable *pTable = pRow->pTable;
  if (pTable == NULL) return TSDB_CODE_MND_SDB_INVALID_TABLE_TYPE;

  if (sdbGetRowFromObj(pTable, pRow->pObj)) {
    sdbError("vgId:1, sdb:%s, failed to insert:%s since it exist", pTable->name, sdbGetRowStr(pTable, pRow->pObj));
    sdbDecRef(pTable, pRow->pObj);
    return TSDB_CODE_MND_SDB_OBJ_ALREADY_THERE;
  }

  if (pTable->keyType == SDB_KEY_AUTO) {
    *((uint32_t *)pRow->pObj) = atomic_add_fetch_32(&pTable->autoIndex, 1);

    // let vgId increase from 2
    if (pTable->autoIndex == 1 && pTable->id == SDB_TABLE_VGROUP) {
      *((uint32_t *)pRow->pObj) = atomic_add_fetch_32(&pTable->autoIndex, 1);
    }
  }

  int32_t code = sdbInsertHash(pTable, pRow);
  if (code != TSDB_CODE_SUCCESS) {
    sdbError("vgId:1, sdb:%s, failed to insert:%s into hash", pTable->name, sdbGetRowStr(pTable, pRow->pObj));
    return code;
  }

  // just insert data into memory
  if (pRow->type != SDB_OPER_GLOBAL) {
    return TSDB_CODE_SUCCESS;
  }

  if (pRow->fpReq) {
    return (*pRow->fpReq)(pRow->pMsg);
  } else {
    return sdbWriteRowToQueue(pRow, SDB_ACTION_INSERT);
  }
}

// 向写队列中插入SSdbRow
static int32_t sdbWriteRowToQueue(SSdbRow *pInputRow, int32_t action) {
  SSdbTable *pTable = pInputRow->pTable;
  if (pTable == NULL) return TSDB_CODE_MND_SDB_INVALID_TABLE_TYPE;

  int32_t  size = sizeof(SSdbRow) + sizeof(SWalHead) + pTable->maxRowSize;
  SSdbRow *pRow = taosAllocateQitem(size);
  if (pRow == NULL) {
    return TSDB_CODE_VND_OUT_OF_MEMORY;
  }

  memcpy(pRow, pInputRow, sizeof(SSdbRow));
  pRow->processedCount = 1;

  SWalHead *pHead = &pRow->pHead;
  pRow->rowData = pHead->cont;
  (*pTable->fpEncode)(pRow);

  pHead->len = pRow->rowSize;
  pHead->version = 0;
  pHead->msgType = pTable->id * 10 + action;

  return sdbWriteToQueue(pRow, TAOS_QTYPE_RPC);
}

```

```c
// 工作线程会定期写文件，按照策略flush到磁盘
static void *sdbWorkerFp(void *pWorker) {
  SSdbRow *pRow;
  int32_t  qtype;
  void *   unUsed;

  taosBlockSIGPIPE();
  setThreadName("sdbWorker");

  while (1) {
    int32_t numOfMsgs = taosReadAllQitemsFromQset(tsSdbWQset, tsSdbWQall, &unUsed);
    if (numOfMsgs == 0) {
      sdbDebug("qset:%p, sdb got no message from qset, exiting", tsSdbWQset);
      break;
    }

    for (int32_t i = 0; i < numOfMsgs; ++i) {
      taosGetQitem(tsSdbWQall, &qtype, (void **)&pRow);
      sdbTrace("vgId:1, msg:%p, row:%p hver:%" PRIu64 ", will be processed in sdb queue", pRow->pMsg, pRow->pObj,
               pRow->pHead.version);

      pRow->code = sdbProcessWrite((qtype == TAOS_QTYPE_RPC) ? pRow : NULL, &pRow->pHead, qtype, NULL);
      if (pRow->code > 0) pRow->code = 0;

      sdbTrace("vgId:1, msg:%p is processed in sdb queue, code:%x", pRow->pMsg, pRow->code);
    }

    walFsync(tsSdbMgmt.wal, true);

    ......
  }

  return NULL;
}

```

### mnode存储模型

1. mnode的所有表的数据存储
    - 每个表的最新的row数据存在SSdbTable.iHandle这个hash表中
    - 每个表的create/update/insert等操作都通过append方式追加到了wal

## mnode的启动流程
<p align="center">
  <img width="900" height = "800" src="../res/asc-img/%5BPart.04.A.TDengine-why_TDengine_start_slow_when_too_many_ddl_accumulated%5D%20P2%20-%20Start%20of%20mnode.png" alt="flow">
</p>
<p align="center">P2 - flow of mnode start</p>

在mnode的启动流程中最重要的两个步骤：
1. sdbInitWal
2. sdbRestoreTables

步骤1，初始化wal，并且读取wal所有的log，将数据恢复到内存中

步骤2，恢复表的数据（比如行数技术等）

## 结论分析

当有大量的DDL以及acc等表的DML操作时，会导致wal存储了大量的log，这些log需要在启动的时候被全部加载到内存。

这个过程的时间消耗，在wal比较大时，对于线上系统几乎是不能接受的。

当然官方提供了compact命令，但是治标不治本，不能从根本上解决问题。

不过按照TDengine的官方说法，应该会在7月份的3.0版本进行优化：
> 此外，在未来的 TDengine 3.0 版本中，这也会是我们的重大的优化项。由于 WAL 也会变成分布式的存储，届时，即使是在亿级别表数量的情况下，TDengine 的启停速度也都不再会是问题。而且这项优化不过是 3.0 版本诸多特性的冰山一角。这项调整的背后代表着 TDengine 对于很多重要模块的优化重构，稳定性和性能都会大幅提高，多项重磅功能也会上线。
