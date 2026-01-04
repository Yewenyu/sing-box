package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/user"
	"strconv"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/experimental/clash"
	"github.com/sagernet/sing-box/experimental/deprecated"
	"github.com/sagernet/sing-box/include"
	l "github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing/service"
	"github.com/sagernet/sing/service/filemanager"
)

var configFile = ""

var boxService *box.Box

var globalCtx = context.Background()

func main() {

	configFile = "config.json"

	sudoUser := os.Getenv("SUDO_USER")
	sudoUID, _ := strconv.Atoi(os.Getenv("SUDO_UID"))
	sudoGID, _ := strconv.Atoi(os.Getenv("SUDO_GID"))
	if sudoUID == 0 && sudoGID == 0 && sudoUser != "" {
		sudoUserObject, _ := user.Lookup(sudoUser)
		if sudoUserObject != nil {
			sudoUID, _ = strconv.Atoi(sudoUserObject.Uid)
			sudoGID, _ = strconv.Atoi(sudoUserObject.Gid)
		}
	}
	if sudoUID > 0 && sudoGID > 0 {
		globalCtx = filemanager.WithDefault(globalCtx, "", "", sudoUID, sudoGID)
	}
	globalCtx = include.Context(service.ContextWith(globalCtx, deprecated.NewStderrManager(l.StdLogger())))
	go listenConfig()

	reload()
	// ctx := clash.BaseContext(nil)
	go func() {
		time.Sleep(5 * time.Second)
		reload()
	}()

	// go tool pprof -http=:8081 http://localhost:6060/debug/pprof/goroutine
	// go tool pprof http://localhost:6060/debug/pprof/heap
	http.ListenAndServe(":6060", nil)
}

func reload() {
	if boxService != nil {
		boxService.Close()
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		log.Fatal(err)
	}
	content := string(data)
	ctx := globalCtx
	options, err := clash.ParseConfig(ctx, content)
	if err != nil {
		log.Fatal(err)
	}

	server, err := box.New(box.Options{
		Context:           ctx,
		Options:           options,
		PlatformLogWriter: &LogWriter{},
	})

	if err != nil {
		log.Fatal(err)
	}

	err = server.Start()
	if err != nil {
		log.Fatal(err)
	}
	boxService = server

}

func listenConfig() {
	// TCP 端口和地址设置
	tcpAddress := "0.0.0.0:9876"

	// 监听 TCP
	listener, err := net.Listen("tcp", tcpAddress)
	if err != nil {
		fmt.Printf("Failed to listen on TCP port: %v\n", err)
		return
	}
	defer listener.Close()
	fmt.Println("Listening on", tcpAddress)

	for {
		// 接受连接
		conn, err := listener.Accept()
		if err != nil {
			fmt.Printf("Failed to accept connection: %v\n", err)
			continue
		}
		fmt.Printf("Connection accepted from %s\n", conn.RemoteAddr().String())

		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	// 缓冲区用于读取数据
	buffer := make([]byte, 20240)

	// 读取数据直到连接关闭
	for {
		n, err := conn.Read(buffer)

		if err != nil {
			if err != io.EOF {
				fmt.Printf("Failed to read from connection: %v\n", err)
			}
			break
		}
		buf := buffer[:n]
		for {
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			buffer := make([]byte, 20240)
			n, err := conn.Read(buffer)
			if err != nil {
				if err != io.EOF {
					fmt.Printf("Failed to read from connection: %v\n", err)
				}
				break
			}
			b := buffer[:n]
			buf = append(buf, b...)
		}

		// 将接收到的数据写入文件
		if err := os.WriteFile(configFile, buf, 0644); err != nil {
			fmt.Printf("Failed to write to file: %v\n", err)
			continue
		}

		fmt.Printf("Config data written to configFile.txt\n")

		reload()
	}
}

type LogWriter struct {
}

func (l *LogWriter) WriteMessage(level l.Level, message string) {
	log.Printf("%s: %s", level, message)
}
func (l *LogWriter) DisableColors() bool {
	return false
}
