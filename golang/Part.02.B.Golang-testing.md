# Go - 单元测试
TDD是保证项目质量的一个好的办法，即使你的项目不使用TDD，那基础的测试覆盖率也是在不停的项目迭代后保证交付
质量的一个重要指标。在整个测试金字塔中，单元测试、集成测试承担了重要的作用。

集成测试，在项目过程中，依赖于整个项目情况，所以本篇主要列举我在项目开发过程中一些常用的单元测试方法。

下面的示例代码都位于我的示例工程中: 
[**示例工程**](https://github.com/lj-211/go-test-example)

## Unit Test
### 单元测试常用的场景
因为单元测试的最小单位是你的目标函数，但是函数可能会依赖于其他的接口，这些接口可能是db function等，
为了让我们的单元测试按照我们构造的数据和逻辑，覆盖全目标函数所有代码，我们就需要模拟这些依赖。

- mock function
- mock method
- mock http
- mock db data
- mock grpc

以上这些就是我的开发过程中常用的场景，后面我主要会以代码片段的形式展示。

同样还有一些test库对官方的testing包进行补充，提供了一些断言、判断以及测试套件等功能也是很好用，
后面也有例子说明。

### 增强的测试包
testify库作为官方的testing包补充提供了一些常用的assert、require等函数，方便的用于结果的判定。
```
// 判断错误
assert.Nil(t, err)
assert.NotNil(t, err)
// 判定函数返回结果
assert.Equal(t, false, ObjTest(), "必须返回false")
assert.NotEqual(123, 456, "they should not be equal")
// 根据判定结果终止测试
requre.Nil(t, err)
```
testify还提供了测试套件的封装，后面的部分有详述。

### mock mysql
这个例子中，通过对mysql driver的mock来伪造我们想从db获取的数据，这样我们可以轻易构造，我们需要的
数据来完成测试或者构造db返回的值完成所有代码分支的测试覆盖。

```
func TestModel_CreateReceiptFail(t *testing.T) {
    mdb, mock, err := sqlmock.New()
    require.Nil(t, err)

    db, err := gorm.Open("mysql", mdb)
    require.Nil(t, err)
    defer mdb.Close()

    model := Model{
            One: "one",
            Two: 2,
    }
    // 1. insert ok
    mock.ExpectBegin()
    mock.ExpectExec("^INSERT  INTO `model` (.+)$").WillReturnResult(sqlmock.NewResult(1, 1))
    mock.ExpectCommit()
    err = SaveModel(db, &model)
    if !assert.Nil(t, err) {
            log.Println("正常保存预期外的错误: ", err.Error())
    }
    // 2. 测试插入失败
    mock.ExpectBegin()
    mock.ExpectExec("^INSERT  INTO `model` (.+)$").WillReturnError(errors.New("fail"))
    mock.ExpectCommit()

    err = SaveModel(db, &model)
    if assert.NotNil(t, err) {
            assert.Equal(t, "fail", errors.Cause(err).Error(), "返回错误消息必须是fail")
            log.Println(err.Error())
    }
}
```

### mock http server
```
data := TestData{Val: 1234}
dataStr, _ := json.Marshal(data)
server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,
	r *http.Request) {
	// 在这里mock需要的数据
	w.WriteHeader(200)
	io.WriteString(w, string(dataStr))
}))
defer server.Close()

// 替换请求的地址未server.URL
```

### mock function & method
```
func GetNum(a, b int) int {
    fmt.Println("call GetNum")
    return a + b
}

func MethodTest(a, b int) int {
    return GetNum(a, b) + 100
}

type TestObj struct {
}

func (self *TestObj) LessZero(in int) bool {
    if in < 0 {
            return true
    }

    return false
}

func ObjTest() bool {
    obj := &TestObj{}
    return obj.LessZero(100)
}
```

```
// mock func
func MockGetNum(a, b int) int {
    log.Println("call MockGetNum")
    return 2
}

// 我们在MethodTest函数的单元测试用，需要mock GetNum的函数行为。
func Test_MethodTest(t *testing.T) {
    patches := gomonkey.ApplyFunc(GetNum, MockGetNum)
    defer patches.Reset()

    ret := MethodTest(1, 3)

    assert.Equal(t, 102, ret, "返回值必须是102")
}

// 模拟TestObj对象的LessZero方法
func Test_ObjTest(t *testing.T) {
    obj := &TestObj{}
    patches := gomonkey.ApplyMethod(reflect.TypeOf(obj), "LessZero", func(self *TestObj, in int) bool {
            return false
    })
    defer patches.Reset()
    assert.Equal(t, false, ObjTest(), "必须返回false")
}
```

### mock interface
对于interface的mock,我们使用gomock这个库，我们例子中使用源码mock的方式，使用mockgen生成我们需要mock的interface对应的mock代码，在我们的例子中我们的mock的代码命令生成在./mock/mock.go文件。
```
import (
	"github.com/lj-211/go-test-example/mock"
)

func Test_InterfaceFunc(t *testing.T) {
    ctrl := gomock.NewController(t)
    defer ctrl.Finish()

    mockObj := mock.NewMockPrinter(ctrl)
    gomock.InOrder(
            mockObj.EXPECT().Add(gomock.Any(), gomock.Any()).Return(2),
            mockObj.EXPECT().PlusTwo(gomock.Any()).Return(4),
            mockObj.EXPECT().Add(1, 1).Return(4),
    )

    assert.Equal(t, 2, mockObj.Add(1, 2))
    assert.Equal(t, 4, mockObj.PlusTwo(1))
    assert.NotEqual(t, 2, mockObj.Add(1, 1))
}
```

在我们模拟grpc的请求时，也可以使用gomock来进行，在例子工程中，grpc使用的pb文件位于nettest/proto/example.pb.go，我们对这个源码进行mock后可以直接使用mock对象来进行模拟grpc请求。

```
func Test_CallGrpcClient(t *testing.T) {
    ctrl := gomock.NewController(t)
    defer ctrl.Finish()

    mockObj := mock.NewMockHelloClient(ctrl)
    gomock.InOrder(
            mockObj.EXPECT().SayHello(context.Background(), gomock.Any()).Return(
                    &proto.HelloRes{
                            Id: 1234,
                    }, nil,
            ),
    )

    // grpc调用
    res, err := mockObj.SayHello(context.Background(), &proto.HelloReq{
            Msg: "测试",
    })
    assert.Nil(t, err)
    assert.Equal(t, uint32(1234), res.Id, "返回值必须是1234")
}
```

### suite test
```
type ExampleTestSuite struct {
        suite.Suite
        OldVariableThatShouldStartAtFive int
}

func (suite *ExampleTestSuite) SetupTest() {
        suite.OldVariableThatShouldStartAtFive = VariableThatShouldStartAtFive
        VariableThatShouldStartAtFive = 5
}

func (suite *ExampleTestSuite) TestExample() {
        assert.Equal(suite.T(), 5, VariableThatShouldStartAtFive)
        suite.Equal(5, VariableThatShouldStartAtFive)
}

func (suite *ExampleTestSuite) TearDownTest() {
        // 进行资源释放或者数据还原
        VariableThatShouldStartAtFive = suite.OldVariableThatShouldStartAtFive
}

func (suite *ExampleTestSuite) BeforeTest() {
        // 测试前置处理
}

func (suite *ExampleTestSuite) AfterTest() {
        // 测试后置处理
}

func TestExampleTestSuite(t *testing.T) {
        suite.Run(t, new(ExampleTestSuite))

        log.Println("全局变量为: ", VariableThatShouldStartAtFive)
}
```

这部分测试方便我们在测试时，组织我们的测试代码。
比如一个下单接口，需要构造很多的测试数据，构造各种边界测试条件数据，那么我们可以SetupTest时，新建一个测试db,
然后在这个db中灌入我们需要的数据，然后在TearDownTest中删除db即可。


## tricks
### 通过表驱动提高覆盖率
以下是标准库中string_test部分对于表驱动测试的返利
```
var atoi64tests = []atoi64Test{
	{"", 0, false},
	{"0", 0, true},
	{"-0", 0, true},
	{"1", 1, true},
	{"-1", -1, true},
	{"12345", 12345, true},
	{"-12345", -12345, true},
	{"012345", 12345, true},
	{"-012345", -12345, true},
	{"12345x", 0, false},
	{"-12345x", 0, false},
	{"98765432100", 98765432100, true},
	{"-98765432100", -98765432100, true},
	{"20496382327982653440", 0, false},
	{"-20496382327982653440", 0, false},
	{"9223372036854775807", 1<<63 - 1, true},
	{"-9223372036854775807", -(1<<63 - 1), true},
	{"9223372036854775808", 0, false},
	{"-9223372036854775808", -1 << 63, true},
	{"9223372036854775809", 0, false},
	{"-9223372036854775809", 0, false},
}

func TestAtoi(t *testing.T) {
	switch intSize {
	case 32:
		for i := range atoi32tests {
			test := &atoi32tests[i]
			out, ok := runtime.Atoi(test.in)
			if test.out != int32(out) || test.ok != ok {
				t.Errorf("atoi(%q) = (%v, %v) want (%v, %v)",
					test.in, out, ok, test.out, test.ok)
			}
		}
	case 64:
		for i := range atoi64tests {
			test := &atoi64tests[i]
			out, ok := runtime.Atoi(test.in)
			if test.out != int64(out) || test.ok != ok {
				t.Errorf("atoi(%q) = (%v, %v) want (%v, %v)",
					test.in, out, ok, test.out, test.ok)
			}
		}
	}
}
```
### 复杂数据使用文件替代
https://golang.org/src/cmd/gofmt/gofmt_test.go
```
// 我们可以使用文件来代替负责输出检查
var update = flag.Bool("update", false, "update .golden files")
func TestSomething(t *testing.T) {
  actual := doSomething()
  golden := filepath.Join(“testdata”, tc.Name+”.golden”)
  if *update {
    ioutil.WriteFile(golden, actual, 0644)
  }
  expected, _ := ioutil.ReadFile(golden)
 
  if !bytes.Equal(actual, expected) {
    // FAIL!
  }
}
```
### 区分你的测试
在测试文件中的首行加入 // +build unit_test
在执行测试用例时使用go test -tags=unit_test就可以单独跑加入了tag的测试。
### 要不要使用独立的包名
使用单独包名的例子之一就是标准库的string_test.go。
至于在你的项目中要不要使用这个方式取决于你的测试是黑盒还是白盒。

## tips
### 保证你项目的test coverage
go test -v -coverprofile cover.out user_test.go user.go
go tool cover -html=cover.out -o cover.html 

## reference
### 常用的test和mock功能库
- github.com/DATA-DOG/go-sqlmock
- github.com/golang/mock/gomock
- github.com/stretchr/testify
- net/http/httptest
- github.com/agiledragon/gomonkey

### 一些有用的库
- 解决数据库并发测试问题 https://github.com/khaiql/dbcleaner
- 构造fake data https://github.com/Pallinder/go-randomdata
