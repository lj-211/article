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

## mnode的启动流程
<p align="center">
  <img width="900" height = "800" src="../res/asc-img/%5BPart.04.A.TDengine-why_TDengine_start_slow_when_too_many_ddl_accumulated%5D%20P2%20-%20Start%20of%20mnode.png" alt="flow">
</p>
<p align="center">P2 - flow of mnode start</p>

## 结论分析

