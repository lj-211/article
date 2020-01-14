# 如何阅读man文档的SYNOPSIS
## 基本概念
### option
> The arguments that consist of <hyphen-minus> characters and single letters or digits, such as 'a', are known as "options" (or, historically, "flags").

有两种形式: 
* 长选项 
    * 用--开头，后面跟随完整的单词，例如: --help
	* 长选项不可以组合使用	
* 短选项
	* 用-开头，后台跟单个字符，例如: -a
	* 短选项可以组合使用，例如: ls -silh

**NOTE**

1. 除非特殊说明，选项之间没有依赖关系
2. 重复的选项的结果是未定义的

### option-arguments
> Certain options are followed by an "option-argument", as shown with [ -c option_argument]

选项参数，跟在options后面的被称为选项参数
* 选项参数和选项之间使用空格分隔，除非选项参数被"[]"包围表示是可选项
* [-c option_argument] 这种情况，如果提供option_argument，则应该和option之间以空格间隔
* -f[opton_argument] 这种情况，option和option_argument之间应该丢弃空格，直接相邻；
* 需要用实际值替换的参数的名称用嵌入的<parameter_name>字符表示

### operands
> The arguments following the last options and option-arguments are named "operands".

## 规则
### 基础规则
1. '[' and ']'中间的option或者option_argument都是可选项
2. 被'|'分隔的参数是互斥的
3. 相互排斥的选项和操作数可以用多个SYNOPSIS列出，参见reference.3
4. "..."跟随在option或者operand之后，标识可以有零或者多个option或者operand被指定，参见reference.4

### 不同的展示方式
少量参数: utility_name [-abcDxyz][-p arg][operand]

复杂参数:	 utility_name [options][operands]

## references
1. [Utility Conventions](https://pubs.opengroup.org/onlinepubs/9699919799/basedefs/V1_chap12.html)
2. [IEEE Std 1003.1, 2013 Edition](http://ecee.colorado.edu/~ecen5653/ecen5653/papers/POSIX-1003.1/basedefs/V1_chap01.html#tag_01)
3. 多个互斥SYNOPSIS展示
	3.1 utility_name -d[-a][-c option_argument][operand...]
	3.2 utility_name [-a][-b][operand...]
4. utility_name [-g option_argument]...[operand...]
