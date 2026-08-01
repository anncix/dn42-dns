package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"smartdns/internal/config"
	"smartdns/internal/server"
)

var (
	version = "2.2.0"
	build   = "dev"
)

func main() {
	configPath := flag.String("c", "configs/dn42.yaml", "配置文件路径")
	showVersion := flag.Bool("v", false, "显示版本信息")
	flag.Parse()

	if *showVersion {
		fmt.Printf("dn42-dns v%s (build: %s)\n", version, build)
		fmt.Println("面向 dn42 网络的轻量级 DNS 分流服务器")
		return
	}

	fmt.Printf("========================================\n")
	fmt.Printf("  dn42-dns v%s\n", version)
	fmt.Printf("  Lightweight DNS for dn42\n")
	fmt.Printf("========================================\n\n")

	// 加载配置
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		fmt.Printf("加载配置失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("配置文件: %s\n", *configPath)

	// 创建服务器
	srv, err := server.NewServer(cfg)
	if err != nil {
		fmt.Printf("创建服务器失败: %v\n", err)
		os.Exit(1)
	}

	// 启动服务器
	if err := srv.Start(); err != nil {
		fmt.Printf("启动服务器失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n服务器启动成功！")
	fmt.Println("  Ctrl+C  - 停止服务")
	fmt.Println("  SIGUSR1 - 清空DNS缓存")

	// 等待退出信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGUSR1,
	)

	for {
		sig := <-sigChan
		switch sig {
		case syscall.SIGINT, syscall.SIGTERM:
			fmt.Println("\n正在停止服务器...")
			srv.PrintStats()
			srv.Stop()
			fmt.Println("服务器已停止")
			return

		case syscall.SIGUSR1:
			fmt.Println("\n收到 SIGUSR1 信号，清空DNS缓存...")
			srv.FlushCache()
		}
	}
}
