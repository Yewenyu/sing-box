package clash

import (
	"fmt"
	"net"
	"net/http"
)

func SendConfig(tcpAddress string, configData string) string {
	// 建立TCP连接
	conn, err := net.Dial("tcp", tcpAddress)
	if err != nil {
		return fmt.Sprintf("dialing TCP failed: %v", err)
	}
	defer conn.Close()

	// 发送数据
	_, err = conn.Write([]byte(configData))
	if err != nil {
		return fmt.Sprintf("sending data failed: %v", err)
	}

	fmt.Printf("Config data sent to %s\n", tcpAddress)
	return ""
}

func PProf(address string) {
	go http.ListenAndServe(address, nil)
}
