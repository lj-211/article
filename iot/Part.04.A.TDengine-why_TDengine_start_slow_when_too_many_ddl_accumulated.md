# TDengine的mnode的启动逻辑

## 背景知识
这部分源码用到了两个基础工具类，请阅读前阅读以下两篇文章:
[**TDengine SHashObj**](https://github.com/lj-211/article/blob/master/iot/Part.04.A.TDengine-util_hashtable.md)
[**TDengine SRefSet**](https://github.com/lj-211/article/blob/master/iot/Part.04.A.TDengine-util_tref.md)

## mnode主要数据结构
<p align="center">
  <img width="1008" height = "700" src="https://github.com/lj-211/article/blob/master/res/asc-img/%5BPart.04.A.TDengine-why_TDengine_start_slow_when_too_many_ddl_accumulated%5D%20P1%20-%20Data%20Structure.png?raw=true" alt="data">
</p>
<p align="center">P1 - mnode data struture</p>

## mnode的启动流程
<p align="center">
  <img width="900" height = "800" src="https://github.com/lj-211/article/blob/master/res/asc-img/%5BPart.04.A.TDengine-why_TDengine_start_slow_when_too_many_ddl_accumulated%5D%20P2%20-%20Start%20of%20mnode.png?raw=true" alt="flow">
</p>
<p align="center">P2 - flow of mnode start</p>
