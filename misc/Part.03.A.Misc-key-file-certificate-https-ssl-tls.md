# 从非对称加密到https
写本文的触发是因为有一天突然去细想https协议，突然发现自己对于一些基本概念其实并没有完全掌握，很多细节只有个模糊的印象；
所以为了证明我懂了这些基本概念，又从头去梳理了一下知识点。

本文会按照非对称加密 - 数字证书 - ssl/tls - https的顺序去解释一些基本概念及原理，所以本文注定是一篇长篇大作。如果你对部分
章节很熟悉，那完全可以直接跳过。

## 非对称加密以及秘钥
因为SSL/TSL协议的的会话秘钥之前是非对称加密，所以开篇也是说非对称加密。
非对称加密指的是加密和解密过程适用不同的秘钥进行加解密的加密算法也叫双钥算法。

对于秘钥而言就不得不提ASN.1，ASN.1: 全称Abstract Syntax Notation One，是一种描述数字对象的标准。
ASN.1常用的编码方案有BER DER PER XER等等。
由于BER编码有不唯一性的特点，所以更长使用的是DER(Distinguished Encoding Rules).
PEM编码: Privacy Enhanced Mail，是一种保密邮件的编码标准。实际上PEM是对DER进行base64编码后，在头和尾加上标注。

说到秘钥格式就绕不开PKCS。
The Public-Key Cryptography Standards (PKCS)是由美国RSA数据安全公司及其合作伙伴制定的一组公钥密码学标准，其中包括证书申请、证书更新、证书作废表发布、扩展证书内容以及数字签名、数字信封的格式等方面的一系列相关协议。

> PKCS#1：定义RSA公开密钥算法加密和签名机制，主要用于组织PKCS#7中所描述的数字签名和数字信封。 
> PKCS#3：定义Diffie-Hellman密钥交换协议。 
> PKCS#5：描述一种利用从口令派生出来的安全密钥加密字符串的方法。使用MD2或MD5从口令中派生密钥，并采用DES-CBC模式加密。主要用于加密从一个计算机传送到另一个计算机的私人密钥，不能用于加密消息。 
> PKCS#6：描述了公钥证书的标准语法，主要描述X.509证书的扩展格式。 
> PKCS#7：定义一种通用的消息语法，包括数字签名和加密等用于增强的加密机制，PKCS#7与PEM兼容，所以不需其他密码操作，就可以将加密的消息转换成PEM消息。 
> PKCS#8：描述私有密钥信息格式，该信息包括公开密钥算法的私有密钥以及可选的属性集等。 
> PKCS#9：定义一些用于PKCS#6证书扩展、PKCS#7数字签名和PKCS#8私钥加密信息的属性类型。 
> PKCS#10：描述证书请求语法。 
> PKCS#11：称为Cyptoki，定义了一套独立于技术的程序设计接口，用于智能卡和PCMCIA卡之类的加密设备。 
> PKCS#12：描述个人信息交换语法标准。描述了将用户公钥、私钥、证书和其他相关信息打包的语法。 
> PKCS#13：椭圆曲线密码体制标准。 
> PKCS#14：伪随机数生成标准。 
> PKCS#15：密码令牌信息格式标准。 

```
// PKCS#1 - rsa特定格式
// RSA public key
-----BEGIN RSA PUBLIC KEY-----
BASE64 ENCODED DATA
-----END RSA PUBLIC KEY-----
// RSA private key
-----BEGIN RSA PRIVATE KEY-----
BASE64 ENCODED DATA
-----END RSA PRIVATE KEY-----
```

```
// PKCS#8
// 因为RSA并不是一定在SSL/TSL协议中使用所以另外一种格式更常使用
// 公钥
-----BEGIN PUBLIC KEY-----
BASE64 ENCODED DATA
-----END PUBLIC KEY-----

// 加密的私钥
-----BEGIN ENCRYPTED PRIVATE KEY-----
BASE64 ENCODED DATA
-----END ENCRYPTED PRIVATE KEY-----
// 非加密的私钥
-----BEGIN PRIVATE KEY-----
BASE64 ENCODED DATA
-----END PRIVATE KEY-----
```

> PKCS#8的公钥和私钥的格式如下

```
// public 
PublicKeyInfo ::= SEQUENCE {
  algorithm       AlgorithmIdentifier,
  PublicKey       BIT STRING
}
 
AlgorithmIdentifier ::= SEQUENCE {
  algorithm       OBJECT IDENTIFIER,
  parameters      ANY DEFINED BY algorithm OPTIONAL
}
// private
PrivateKeyInfo ::= SEQUENCE {
  version         Version,
  algorithm       AlgorithmIdentifier,
  PrivateKey      BIT STRING
}
 
AlgorithmIdentifier ::= SEQUENCE {
  algorithm       OBJECT IDENTIFIER,
  parameters      ANY DEFINED BY algorithm OPTIONAL
}

// 如果算法使用的是RSA，那么BIT STRING存储的就是rsa key。
```

## 数字证书
### 数字签名和数字证书
数字签名和数字证书这两个概念比较模糊，我用一张图说明他们的关系。

```
// TODO 图示数字证书和数字签名
```

数字证书是指CA机构发行的一种电子文档，是一串能够表明网络用户身份信息的数字，提供了一种在计算机网络上验证网络用户身份的方式，因此数字证书又称为数字标识。

### 数字证书格式
x509 PKCS7 PKCS12
一直对这三个概念比较模糊，x509是一套证书标准，而PKCS7和PKCS12是具体的一种格式。
PKCS7把公钥和私钥分文件存放；PKCS12把公钥和私钥存放在一起，可以进行密码保护，pfx文件就是PKCS12格式的证书文件。

证书的格式如下(PKCS12是二进制格式)
```
// certificate
-----BEGIN CERTIFICATE----- 
BASE64 ENCODED DATA
-----END CERTIFICATE----- 
```

## https协议
https协议就是数字证书应用的一个典型案例。

### https怎么验证CA证书
TODO GO源码 & 浏览器 & OCSP stapling

### SSL证书
目前主流的SSL证书的种类有DV OV EV三种。他们的核心区别如下：
- DV不包含组织信息	
- OV包含组织信息
- EV包含最严格检查的组织信息，浏览器提示证书时会显示绿色

## SSL/TLS协议
RFC文档 https://tools.ietf.org/html/rfc5246
- 窃听风险（eavesdropping）：第三方可以获知通信内容。
- 篡改风险（tampering）：第三方可以修改通信内容。
- 冒充风险（pretending）：第三方可以冒充他人身份参与通信。

SSL/TSL协议的设计目的:
- 所有信息都是加密传播，第三方无法窃听。
- 具有校验机制，一旦被篡改，通信双方会立刻发现。
- 配备身份证书，防止身份被冒充。
### 协议流程
TODO
### 一些特殊的点
- 双向认证
- go的tls实现

```
RootCAS - FetchPEMRoots

cs, ok := msg.(*certificateStatusMsg)
if ok {
	// RFC4366 on Certificate Status Request:
	// The server MAY return a "certificate_status" message.

	if !hs.serverHello.ocspStapling {
		// If a server returns a "CertificateStatus" message, then the
		// server MUST have included an extension of type "status_request"
		// with empty "extension_data" in the extended server hello.

		c.sendAlert(alertUnexpectedMessage)
		return errors.New("tls: received unexpected CertificateStatus message")
	}
	hs.finishedHash.Write(cs.marshal())

	c.ocspResponse = cs.response

	msg, err = c.readHandshake()
	if err != nil {
		return err
	}
}
```

## reference
1. [ASN.1 key structures in DER and PEM](https://tls.mbed.org/kb/cryptography/asn1-key-structures-in-der-and-pem)
2. [数字签名是什么](http://www.ruanyifeng.com/blog/2011/08/what_is_a_digital_signature.html)
3. [图解数字签名](http://www.youdzone.com/signature.html)
4. [SSL/TLS协议](https://www.ruanyifeng.com/blog/2014/02/ssl_tls.html)
