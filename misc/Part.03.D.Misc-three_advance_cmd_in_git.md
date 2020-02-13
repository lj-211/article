# GIT三个高级命令
本篇主要从实际场景处方，从开发实际用到的场景中可能碰到的问题出发，来看怎们用git去解决这些问题。

## rebase
很多地方把这个操作翻译为变基；所以字面意思可以看出，这事对git的的提交行为进行变更的行为；能修改git的提交行为，是不是很强大，不过
强大的反面是他也很危险。

### 危害
在应用场景之前，我要先说rebase不可应用的场景，只有明确你不在这个场景下，后面的解决方案对你来说才会有意义。

通常来说，因为rebase会修改单个分支的提交行为，所以rebase只适合你单独管理自己的分支。如果你需要与别人在同一个分支协作开发，
那么我建议你还是千万不要使用rebase。

### 整理revision log
在开发过程中，我们可能会commit很多冗余的提交，反映在log上就会出现很多冗余的日志。这样会让整个revision log看着非常混乱冗余。

我用一个场景去描述可能出现的场景:
```
c7e7ef6 (HEAD -> feature/rebase-1) 3-解决编译问题
8c4352a 2-解决编译问题
f876e89 1-解决编译问题
c85e8d9 4
```

可以看到，我们最近的三次提交其实都是为了解决一样的问题，那我们期望我们的log能表现成以下的结果:
```
6b157bf (HEAD -> feature/rebase-1) 1 2 3 解决编译问题
c85e8d9 4
```

这个时候，我们就可以使用git rebase -i c85e8d9进行整理commit
```
# 变基 c85e8d9..c7e7ef6 到 c85e8d9（3 个提交）
#
# 命令:
# p, pick <提交> = 使用提交
# r, reword <提交> = 使用提交，但修改提交说明
# e, edit <提交> = 使用提交，进入 shell 以便进行提交修补
# s, squash <提交> = 使用提交，但融合到前一个提交
# f, fixup <提交> = 类似于 "squash"，但丢弃提交说明日志
# x, exec <命令> = 使用 shell 运行命令（此行剩余部分）
# b, break = 在此处停止（使用 'git rebase --continue' 继续变基）
# d, drop <提交> = 删除提交
# l, label <label> = 为当前 HEAD 打上标记
# t, reset <label> = 重置 HEAD 到该标记
# m, merge [-C <commit> | -c <commit>] <label> [# <oneline>]
# .       创建一个合并提交，并使用原始的合并提交说明（如果没有指定
# .       原始提交，使用注释部分的 oneline 作为提交说明）。使用
# .       -c <提交> 可以编辑提交说明。
```
-i属性表示进行交互rebase。只需要根据提示重新选择对commit的操作进行下去即可。

### 每日同步合作分支代码 git pull --rebase
git pull 默认使用的是git-merge。在和别人同步开发代码时，为了保证我们自己的变更比较干净整洁，我们可以
放弃merge的行为，而使用rebase，暂存本地改动，然后同步其他人的代码后回放自己的改动。

场景描述:
```
A---B---C---D---E  feature
	 \
	  A'---B'---C' 本地改动


A---B---C---D---E---A'---B'---C'
```

可以看到pull之后，我们的log是比较简洁干净，可以对比下log的树形结构
```
// git pull --merge
*   f4868bf (HEAD -> feature/rebase-1) fix conflict
|\                                                                                                                                       
| * 328966f (origin/feature/rebase-1) 2-2
| * 699cebf 2-1
* | c269064 1-2
* | dcde90a 1-1
|/  
*   6d06b3d del

// git pull --rebase
* 545779b (HEAD -> feature/rebase-1) 1-2
* ad96547 1-1
* 328966f (origin/feature/rebase-1) 2-2
* 699cebf 2-1
*   6d06b3d del
```

### 同步源分支代码 git rebase branch
其实rebase branch和rebase revision-id是一样的机制。都是同步对应时刻的和本地对比差异的提交。

当我们开发一个feature，可能开发周期会持续时间比较长，那么为了避免我们在上线的时候和主分支差异过大。我们每天完成开发任务后，git rebase master分支，来尽量缩小和主分支的差异。

### 从依赖分支分叉出去 git rebase onto 
第一个场景:

我们的topic分支是基于next开发，依赖于next分支中部分功能，但是在开发过程中，我们依赖的功能
已经合入了更稳定的master分支，那这个时候我们希望我们基于master来进行开发，那这个时候我们就可以使用

> git rebase --onto master next topic
> 把next和topic分支差异的提交，在master的基础上重放出最新的topic分支。

```
o---o---o---o---o  master
             \
              o---o---o---o---o  next
                               \
                                o---o---o  topic

o---o---o---o---o  master
    |            \
    |             o'--o'--o'  topic
     \
      o---o---o---o---o  next                        
```

第二个场景:

feature为合作开发分支，A基于master分支开发了部分代码在topicA分支，B基于topicA分支开发了部分代码再
topicB分支。但是topicB分支不依赖于topicA分支，这个时候我们临时需要把topicB的内容提前提测和上线。
那这个时候我们就可以

> git rebase --onto master topicA topicB


```
                       H---I---J topicB
                      /
             E---F---G  topicA
            /
A---B---C---D  master

            H'--I'--J'  topicB
           /
           | E---F---G  topicA
           |/
A---B---C---D  master
```

## cherry-pick
关于cherry-pick，阅读下man文档的解释足矣；
这个命令，可以很方便的在开发过程中把我们的commit移动到其他branch。

> git-cherry-pick - Apply the changes introduced by some existing commits
> SYNOPSIS
> git cherry-pick [--edit] [-n] [-m parent-number] [-s] [-x] [--ff]
>                  [-S[<keyid>]] <commit>...
> git cherry-pick --continue
> git cherry-pick --quit
> git cherry-pick --abort

关于如何阅读SYNOPSIS可以参照[Part.03.E.Misc-how_to_read_synopsis_of_man(**如何阅读man文档的SYNOPSIS**)](./Part.03.E.Misc-how_to_read_synopsis_of_man.md)

> 这里需要特殊说明的是<commit>... 存在几种示例如下:

> git cherry-pick master 应用master分支最顶端的commit

> git cherry-pick ..master, git cherry-pick ^HEAD master 应用所有是master祖先而非HEAD祖先的commit

> git cherry-pick maint next ^master, git cherry-pick maint master..next应用maint和next的所有祖先的commit而非master或者其祖先的commit

> git cherry-pick master~4 master~2 应用head^4和head^2两个commit

## diff & patch
### diff和patch文件的差异
> diff是标准的patch文件，使用git diff生成 文件记录文件的差异，可以记录多个commit的差异

> The patch produced by git format-patch is in UNIX mailbox format, with a fixed "magic" time stamp to indicate 
> that the file is output from format-patch rather than a real mailbox.

patch使用git format-patch生成 不仅记录文件差异，还记录了commit的信息，一个patch文件对应一个commit

### 如何使用
git diff 生成patch文件

git apply --stat diff.patch 显示patch文件信息

git apply --check diff.patch 检查patch文件

git format-patch git.patch 生成patch文件

git am git.patch 应用patch文件

### 使用场景
在协作开发中，为了保证代码同步，cherry-pick是解决本地的问题，而patch则可以解决远端的问题，当我们没有代码中心时，或者需要我们自己整理多个commit
为补丁时，那么patch功能，则能很好的进行代码同步。

