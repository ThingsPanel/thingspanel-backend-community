# go-uniqid &nbsp; [![Tweet](https://img.shields.io/twitter/url/http/shields.io.svg?style=social)](https://twitter.com/intent/tweet?text=Simple%20PHP%20uniqid%28%29%20implementation%20in%20Golang.%20&amp;url=https://github.com/mintance/go-uniqid&amp;hashtags=go,php)

[![License: Apache 2](https://img.shields.io/hexpm/l/plug.svg)](https://github.com/mintance/go-uniqid/blob/master/LICENSE)
![Golang Version](https://img.shields.io/badge/golang-1.5%2B-blue.svg)
[![GitHub issues](https://img.shields.io/github/issues/mintance/go-uniqid.svg)](https://github.com/mintance/go-uniqid/issues)
[![Travis CI](https://img.shields.io/travis/mintance/go-uniqid.svg)](https://travis-ci.org/mintance/go-uniqid)

Simple PHP uniqid() implementation in Golang

### How to use

##### Just include our package

```go get github.com/mintance/go-uniqid```

##### See samples

In PHP was:
```php
$id = uniqid("test", true);
```

In Go type:
```go
id := uniqid.New(uniqid.Params{"test", true})
```

