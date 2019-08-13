# Go test
## TODO
- 需要灌数据的怎么测试？提前灌数据？
	- testify suite 提供了前置和后置条件
- 需要检查数据的业务正确性吗？

## mock 
### mock httpserver [net/http/httptest] TODO: 还有的其他功能？
```
data := TestData{Val: 1234}
	dataStr, _ := json.Marshal(data)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,
		r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, string(dataStr))
	}))
	defer server.Close()

	// do request the server.URL
```

## tricks
### Achieving Good Coverage with Table Driven Tests
```
var lastIndexTests = []IndexTest{
    {"", "", 0},
    {"", "a", -1},
    {"", "foo", -1},
    {"fo", "foo", -1},
    {"foo", "foo", 0},
    {"foo", "f", 0},
    {"oofofoofooo", "f", 7},
    {"oofofoofooo", "foo", 7},
    {"barfoobarfoo", "foo", 9},
    {"foo", "", 3},
    {"foo", "o", 2},
    {"abcABCabc", "A", 3},
    {"abcABCabc", "a", 6},
}
```

### Golden files
https://golang.org/src/cmd/gofmt/gofmt_test.go
```
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
This trick allows you to test complex output without hardcoding it.
### 区分测试
Differentiate your Unit and Integration Tests

Note - I originally found out about this tip from: Go Advanced Tips Tricks
If you are writing tests for large enterprise Go systems then you’ll more than likely have a set of both integration and unit tests ensuring the validity of your system.

More often than not however, you’ll find your integration tests taking far longer to run as opposed to your unit tests due to the fact they could be reaching out to other systems.

In this case, it makes sense to put your integration tests into *_integration_test.go files and adding // +build integration to the top of your test file:

「https://tutorialedge.net/golang/advanced-go-testing-tutorial/」
## tips
### 提高cover
go test -v -coverprofile cover.out user_test.go user.go
go tool cover -html=cover.out -o cover.html 
### Use the “underscore test” package ?
根据情况来
1. 白盒 
2. 黑盒
3. 取巧 标准库的string_test.go
### 有用的库
- 造数据 https://github.com/Pallinder/go-randomdata
- 集成测试 https://hackernoon.com/integration-test-with-database-in-golang-355dc123fdc9
- go-sqlmock & grom https://medium.com/@rosaniline/unit-testing-gorm-with-go-sqlmock-in-go-93cbce1f6b5b
