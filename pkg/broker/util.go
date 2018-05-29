package broker

import (
	"fmt"
	"encoding/base64"
)

func i2s(any interface{}) string {
	return fmt.Sprintf("%v", any)
}

func b64decode(data []byte) string {
	str := string(data)
	fmt.Println("before:" + str)
	decoded, err := base64.StdEncoding.DecodeString(str)
	if err != nil {
		panic(err)
	}
	fmt.Println("after:" + string(decoded))
	return string(decoded)
}