## Installation
```
go get github.com/go-basic/uuid
```
## Example
```
package main

import (
	"fmt"
	"github.com/go-basic/uuid"
)

func main()  {
	uuid := uuid.New()
	fmt.Println(uuid)
}
```