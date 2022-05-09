# WAL In TDengine

## WAL文件格式
<p align="center">
  <img width="821" height = "500" src="https://github.com/lj-211/article/blob/master/res/asc-img/%5BPart.04.A.TDengine-WAL_In_TDengine%5D%20P1%20-%20File%20format.png?raw=true" alt="WAL">
</p>
<p align="center">P1 - WAL文件格式</p>

## 核心源码

### 写入磁盘
```c
// 创建WAL的写线程
static int32_t walCreateThread();

// 写线程主循环
static void *walThreadFunc(void *param) {
  int stop = 0;
  setThreadName("wal");
  while (1) {
		// 递增循环次数
    walUpdateSeq();
		// fsync所有的数据文件
    walFsyncAll();

    pthread_mutex_lock(&tsWal.mutex);
    stop = tsWal.stop;
    pthread_mutex_unlock(&tsWal.mutex);
    if (stop) break;
  }

  return NULL;
}

static void walUpdateSeq() {
  taosMsleep(WAL_REFRESH_MS);
  if (++tsWal.seq <= 0) {
    tsWal.seq = 1;
  }
}

// 循环判断所有的引用，判断当前的文件句柄是否需要刷到磁盘
static void walFsyncAll() {
  SWal *pWal = taosIterateRef(tsWal.refId, 0);
  while (pWal) {
    if (walNeedFsync(pWal)) {
      wTrace("vgId:%d, do fsync, level:%d seq:%d rseq:%d", pWal->vgId, pWal->level, pWal->fsyncSeq, tsWal.seq);
      int32_t code = tfFsync(pWal->tfd);
      if (code != 0) {
        wError("vgId:%d, file:%s, failed to fsync since %s", pWal->vgId, pWal->name, strerror(code));
      }
    }
    pWal = taosIterateRef(tsWal.refId, pWal->rid);
  }
}

// 获取文件句柄，并且刷新到磁盘
int32_t tfFsync(int64_t tfd) {
  void *p = taosAcquireRef(tsFileRsetId, tfd);
  if (p == NULL) return -1;

  int32_t fd = (int32_t)(uintptr_t)p;
  int32_t code = taosFsync(fd);

  taosReleaseRef(tsFileRsetId, tfd);
  return code;
}
```

### 初始化 & 释放
```c
int32_t walInit() {
  int32_t code = 0;
	// TODO: 这里的文件存储结构是什么？为什么引用集合是最小VNODE数量
	// 创建引用集合
  tsWal.refId = taosOpenRef(TSDB_MIN_VNODES, walFreeObj);

	// init mutex
  code = pthread_mutex_init(&tsWal.mutex, NULL);
  if (code) {
    wError("failed to init wal mutex since %s", tstrerror(code));
    return code;
  }

	// 创建刷新磁盘主循环
  code = walCreateThread();
  if (code != TSDB_CODE_SUCCESS) {
    wError("failed to init wal module since %s", tstrerror(code));
    return code;
  }

  wInfo("wal module is initialized, rsetId:%d", tsWal.refId);
  return code;
}

void walCleanUp() {
	// 关闭刷新线程
  walStopThread();
	// 释放引用集合
  taosCloseRef(tsWal.refId);
	// destroy mutex
  pthread_mutex_destroy(&tsWal.mutex);
  wInfo("wal module is cleaned up");
}
```

```c
// 根据配置创建一个WAL文件对象
// 这里多是配置赋值，不做详细描述，只对关键函数注释
void *walOpen(char *path, SWalCfg *pCfg) {
  SWal *pWal = tcalloc(1, sizeof(SWal));
  if (pWal == NULL) {
    terrno = TAOS_SYSTEM_ERROR(errno);
    return NULL;
  }

	// 读取配置
  pWal->vgId = pCfg->vgId;
  pWal->tfd = -1;
  pWal->fileId = -1;
  pWal->level = pCfg->walLevel;
  pWal->keep = pCfg->keep;
  pWal->fsyncPeriod = pCfg->fsyncPeriod;
  tstrncpy(pWal->path, path, sizeof(pWal->path));
  pthread_mutex_init(&pWal->mutex, NULL);

  pWal->fsyncSeq = pCfg->fsyncPeriod / 1000;
  if (pWal->fsyncSeq <= 0) pWal->fsyncSeq = 1;

	// 初始化目录
  if (walInitObj(pWal) != TSDB_CODE_SUCCESS) {
    walFreeObj(pWal);
    return NULL;
  }

	// 把WAL对象加入到RefSet
  pWal->rid = taosAddRef(tsWal.refId, pWal);
   if (pWal->rid < 0) {
    walFreeObj(pWal);
    return NULL;
  }

  wDebug("vgId:%d, wal:%p is opened, level:%d fsyncPeriod:%d", pWal->vgId, pWal, pWal->level, pWal->fsyncPeriod);

  return pWal;
}

// 释放资源
void walClose(void *handle) {
  if (handle == NULL) return;

  SWal *pWal = handle;
  pthread_mutex_lock(&pWal->mutex);
  tfClose(pWal->tfd);
  pthread_mutex_unlock(&pWal->mutex);
  taosRemoveRef(tsWal.refId, pWal->rid);
}

// 更改配置
int32_t walAlter(void *handle, SWalCfg *pCfg) {
  if (handle == NULL) return TSDB_CODE_WAL_APP_ERROR;
  SWal *pWal = handle;

  if (pWal->level == pCfg->walLevel && pWal->fsyncPeriod == pCfg->fsyncPeriod) {
    wDebug("vgId:%d, old walLevel:%d fsync:%d, new walLevel:%d fsync:%d not change", pWal->vgId, pWal->level,
           pWal->fsyncPeriod, pCfg->walLevel, pCfg->fsyncPeriod);
    return TSDB_CODE_SUCCESS;
  }

  wInfo("vgId:%d, change old walLevel:%d fsync:%d, new walLevel:%d fsync:%d", pWal->vgId, pWal->level,
        pWal->fsyncPeriod, pCfg->walLevel, pCfg->fsyncPeriod);

  pWal->level = pCfg->walLevel;
  pWal->fsyncPeriod = pCfg->fsyncPeriod;
  pWal->fsyncSeq = pCfg->fsyncPeriod / 1000;
  if (pWal->fsyncSeq <= 0) pWal->fsyncSeq = 1;

  return TSDB_CODE_SUCCESS;
}
```

### 辅助函数
```c
// 提供当前文件id，获取下一个文件id
int32_t walGetNextFile(SWal *pWal, int64_t *nextFileId) {
  int64_t curFileId = *nextFileId;
  int64_t minFileId = INT64_MAX;

  // 读取目录
	// ....

  struct dirent *ent;
  while ((ent = readdir(dir)) != NULL) {
    char *name = ent->d_name;

    if (strncmp(name, WAL_PREFIX, WAL_PREFIX_LEN) == 0) {
			// char*偏移WAL_PREFIX_LEN，取id
      int64_t id = atoll(name + WAL_PREFIX_LEN);
			// 如果id低于当前文件id，则跳过
      if (id <= curFileId) continue;

			// 取最小文件id
      if (id < minFileId) {
        minFileId = id;
      }
    }
  }
  closedir(dir);

	// 写输出参数
  if (minFileId == INT64_MAX) return -1;

  *nextFileId = minFileId;
  wTrace("vgId:%d, path:%s, curFileId:%" PRId64 " nextFileId:%" PRId64, pWal->vgId, pWal->path, curFileId, *nextFileId);

  return 0;
}

// curFileId: 当前文件id
// minDiff: 最小间隔，如果不满足返回-1
// oldFileId: 输出参数
int32_t walGetOldFile(SWal *pWal, int64_t curFileId, int32_t minDiff, int64_t *oldFileId) {
  int64_t minFileId = INT64_MAX;

  // 读取目录
	// ....

  struct dirent *ent;
  while ((ent = readdir(dir)) != NULL) {
    char *name = ent->d_name;

    if (strncmp(name, WAL_PREFIX, WAL_PREFIX_LEN) == 0) {
      int64_t id = atoll(name + WAL_PREFIX_LEN);
			// 如果id大于参数提供的文件id，则
      if (id >= curFileId) continue;

			// 间隔减1
      minDiff--;
			// 取id最小的文件id
      if (id < minFileId) {
        minFileId = id;
      }
    }
  }
  closedir(dir);

  if (minFileId == INT64_MAX) return -1;
	// 如果间隔文件数目小于minDiff，则没有找到文件
  if (minDiff > 0) return -1;

	// 写输出参数
  *oldFileId = minFileId;
  wTrace("vgId:%d, path:%s, curFileId:%" PRId64 " oldFildId:%" PRId64, pWal->vgId, pWal->path, curFileId, *oldFileId);

  return 0;
}

// 获取一个新文件id
// newFileId: 输出参数
// TODO: 为什么是最大文件id而不是最大文件id+1
int32_t walGetNewFile(SWal *pWal, int64_t *newFileId) {
  int64_t maxFileId = INT64_MIN;

	// 读取目录
	// ....

	// 遍历所有文件，取到最大的文件id
  struct dirent *ent;
  while ((ent = readdir(dir)) != NULL) {
    char *name = ent->d_name;

    if (strncmp(name, WAL_PREFIX, WAL_PREFIX_LEN) == 0) {
      int64_t id = atoll(name + WAL_PREFIX_LEN);
      if (id > maxFileId) {
        maxFileId = id;
      }
    }
  }
  closedir(dir);

	// 写输出参数
  if (maxFileId == INT64_MIN) {
    *newFileId = 0;
  } else {
    *newFileId = maxFileId;
  }

  wTrace("vgId:%d, path:%s, newFileId:%" PRId64, pWal->vgId, pWal->path, *newFileId);

  return 0;
}
```

### 文件格式
```c
// handle -> WAL
int32_t walWrite(void *handle, SWalHead *pHead) {
  if (handle == NULL) return -1;

  SWal *  pWal = handle;
  int32_t code = 0;

  // 检查以下三项
  //   1. 文件句柄是否有效
  //   2. WAL对象是否配置记录LOG
  //   3. 新的LOG版本是否大于WAL
  // no wal
  if (!tfValid(pWal->tfd)) return 0;
  if (pWal->level == TAOS_WAL_NOLOG) return 0;
  if (pHead->version <= pWal->version) return 0;

  // 写入checksum
  pHead->signature = WAL_SIGNATURE;
#if defined(WAL_CHECKSUM_WHOLE)
  walUpdateChecksum(pHead);
#else
  pHead->sver = 0;
  taosCalcChecksumAppend(0, (uint8_t *)pHead, sizeof(SWalHead));
#endif

  // 写入长度为 head + head.cont的内存 
  int32_t contLen = pHead->len + sizeof(SWalHead);

  pthread_mutex_lock(&pWal->mutex);

  if (tfWrite(pWal->tfd, pHead, contLen) != contLen) {
    code = TAOS_SYSTEM_ERROR(errno);
    wError("vgId:%d, file:%s, failed to write since %s", pWal->vgId, pWal->name, strerror(errno));
  } else {
    wTrace("vgId:%d, write wal, fileId:%" PRId64 " tfd:%" PRId64 " hver:%" PRId64 " wver:%" PRIu64 " len:%d", pWal->vgId,
           pWal->fileId, pWal->tfd, pHead->version, pWal->version, pHead->len);
    pWal->version = pHead->version;
  }

  pthread_mutex_unlock(&pWal->mutex);

  ASSERT(contLen == pHead->len + sizeof(SWalHead));

  return code;
}
```
