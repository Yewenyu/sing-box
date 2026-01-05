package tunhandler

import (
	"encoding/json"
	"fmt"

	"strings"
	"sync"
	"syscall"

	"github.com/sagernet/sing-box/log"
)

func setSocketBufferSize(fd int, size int) error {
	// 设置接收缓冲区大小
	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_RCVBUF, size); err != nil {
		return fmt.Errorf("failed to set SO_RCVBUF: %v", err)
	}

	// 设置发送缓冲区大小
	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_SNDBUF, size); err != nil {
		return fmt.Errorf("failed to set SO_SNDBUF: %v", err)
	}

	return nil
}
func setNonBlocking(fd int) error {
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(syscall.F_GETFL), 0)
	if errno != 0 {
		return fmt.Errorf("fcntl get failed: %v", errno)
	}

	_, _, errno = syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(syscall.F_SETFL), flags|syscall.O_NONBLOCK)
	if errno != 0 {
		return fmt.Errorf("fcntl set failed: %v", errno)
	}
	return nil
}
func createPipe(nonBlocking bool) (int, int, error) {
	// 创建 socketpair
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_DGRAM, 0)
	if err != nil {
		log.Debug(fmt.Sprintf("Socketpair creation failed: %v\n", err))
		return -1, -1, err
	}

	fd1 := fds[0]
	fd2 := fds[1]

	if nonBlocking {
		// 设置 fd1 和 fd2 为非阻塞模式
		if err := setNonBlocking(fd1); err != nil {
			log.Debug(fmt.Sprintf("Failed to set fd1 non-blocking: %v\n", err))
			return -1, -1, err
		}
		if err := setNonBlocking(fd2); err != nil {
			log.Debug(fmt.Sprintf("Failed to set fd2 non-blocking: %v\n", err))
			return -1, -1, err
		}
	}
	setSocketBufferSize(fd1, 1024*1024)
	setSocketBufferSize(fd2, 1024*1024)

	return fd1, fd2, nil
}

type handleFdFunc = func([]byte) (int, string)
type WriteFunc = func([]byte)

func writeFD(fd int, bytes []byte) {
	// 将数据写入目标，处理部分写入的情况
	for len(bytes) > 0 {
		written, err := syscall.Write(fd, bytes)
		if err != nil {
			// log.Debugln("[tun handle][%s] Write failed: %v\n", tlabel, err)
			continue
		}
		bytes = bytes[written:] // 更新剩余未写入数据
	}
}
func readWrteFD(from, mtu int, flabel string, handle handleFdFunc, writeFunc WriteFunc) {
	go func() {
		buffer := make([]byte, mtu)
		writeChan := make(chan []byte, 10)

		handleWrite := func() {
			for {
				data := <-writeChan
				if writeFunc != nil {
					writeFunc(data)
					continue
				}
				to, _ := handle(data)
				writeFD(to, data)
			}
		}
		go handleWrite()
		// go handleWrite()
		for {
			// 读取数据
			n, err := syscall.Read(from, buffer)
			if err != nil {
				if err == syscall.EAGAIN {
					// 非阻塞模式下没有数据可读时，跳过并继续
					// time.Sleep(100 * time.Millisecond)
					continue
				}
				// log.Debugln("[tun handle][%s] Read failed: %v\n", flabel, err)
				break
			}
			if n > 0 {
				data := append([]byte{}, buffer[:n]...)
				writeChan <- data
			}
		}
	}()
}

type fdPipe struct {
	in, out int
	name    string
}

type OutFD struct {
	DefaultFd int            `json:"default_fd"`
	ProxyFD   map[string]int `json:"proxy_fd"`
}

func (out OutFD) toJsonString() string {
	// 将结构体转换为 JSON 字符串
	jsonData, err := json.Marshal(out)
	if err != nil {
		fmt.Println("Error converting to JSON:", err)
		return ""
	}
	return string(jsonData)
}

func CreateFD(tunFd int, mtu int, ruleProxy string) string {
	defaultKey := "default"

	ruleProxys := []string{defaultKey}

	var handleProxy = false
	starTun := func(logS string) {

	}
	defer func() {
		if !handleProxy {
			go starTun("default tun")
		}
	}()

	if ruleProxy != "" {
		handleProxy = true
		proxys := strings.Split(ruleProxy, ",")
		go starTun(fmt.Sprintf("tun proxys:%s", proxys))
		ruleProxys = append(ruleProxys, proxys...)
	}
	fdMap := make(map[string]*fdPipe)
	outFD := OutFD{ProxyFD: make(map[string]int)}
	for _, r := range ruleProxys {

		fd1, fd2, err := createPipe(true)
		if err != nil {
			log.Debug(fmt.Sprintf("Socketpair creation failed: %v\n", err))
			return ""
		}

		fdMap[r] = &fdPipe{in: fd1, out: fd2, name: r}
		if r == defaultKey {
			outFD.DefaultFd = fd2
		} else {

			outFD.ProxyFD[r] = fd2
		}
	}
	tunName := "tunFd"

	starTun = func(logS string) {
		log.Info("startTun handle proxy: %v", logS)
		proxyDic := make(map[string]*fdPipe)
		var lock sync.Mutex
		readWrteFD(tunFd, mtu, tunName, func(b []byte) (int, string) {

			StartCapture(b)
			// 不在这里先获取defaultKey，先尝试匹配
			p, err := Unpack(b)
			if err == nil && len(fdMap) > 1 {
				v, ok := proxyDic[p.DestinationIPString()]
				if ok {
					return v.in, v.name
				}
				// for k, v := range fdMap {
				// 	// 跳过defaultKey，优先检查其他规则
				// 	if k == defaultKey {
				// 		continue
				// 	}
				// 	if p.Match(k) {
				// 		log.Debugln("[tun handle][rule match]%s match [%s]", p.DestinationIPString(), k)
				// 		lock.Lock()
				// 		proxyDic[p.DestinationIPString()] = v
				// 		lock.Unlock()
				// 		return v.in, v.name
				// 	}
				// }
				fdPipe := fdMap[defaultKey]
				lock.Lock()
				proxyDic[p.DestinationIPString()] = fdPipe
				lock.Unlock()
				return fdPipe.in, fdPipe.name
			}

			// 如果前面没匹配上，就fallback到defaultKey
			fdPipe := fdMap[defaultKey]
			log.Debug(fmt.Sprintf("[tun handle][rule match]%s match [%s]", p.DestinationIPString(), defaultKey))
			return fdPipe.in, fdPipe.name
		}, nil)

		bytesChan := make(chan []byte, len(fdMap)*10)
		go func() {
			for {
				b := <-bytesChan
				StartCapture(b)
				writeFD(tunFd, b)
			}
		}()
		for _, v := range fdMap {
			readWrteFD(v.in, mtu, v.name, nil, func(b []byte) {
				_, err := Unpack(b)
				if err == nil {
					// go p.SetDNSCach()
				}
				newb := append([]byte{}, b...)
				go func(b []byte) { bytesChan <- b }(newb)
			})
		}
	}

	return outFD.toJsonString()
}

type TunTestConfig struct {
	RuleProxy string `json:"rule_proxy"`
	GtsConfig string `json:"gts_config"`
}
type TunBytes struct {
	TunKey string `json:"tun_key"`
	Bytes  []byte `json:"bytes"`
}
type BytesLength struct {
	Length int `json:"length"`
}

var (
	directKey = "default"
	outKey    = "Out"
)

func createTestFD() (int, int) {
	fd1, fd2, err := createPipe(true)
	if err != nil {
		log.Debug(fmt.Sprintf("Socketpair creation failed: %v\n", err))
		return -1, -1
	}
	return fd1, fd2
}
