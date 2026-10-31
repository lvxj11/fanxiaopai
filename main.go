package main

import (
	"flag"
	"fmt"

	"fanxiaopai/database"
	"fanxiaopai/utils"
	"fanxiaopai/web"
)

func main() {
	showVersion := flag.Bool("v", false, "输出版本号")
	flag.Parse()

	if *showVersion {
		fmt.Printf("饭小排 %s v%s\n", utils.FullName, utils.Version)
		return
	}

	config, err := utils.LoadConfig()
	if err != nil {
		fmt.Printf("加载配置失败: %v\n", err)
		return
	}

	if err := utils.InitLoggerWithLevel(config.LogLevel); err != nil {
		fmt.Printf("初始化日志失败: %v\n", err)
		return
	}
	log := utils.GetLogger()
	defer log.Close()

	log.Startup("日志级别: %s\n", config.LogLevel)
	log.Startup("数据库路径: %s\n", config.DBPath)
	log.Startup("Web 端口: %s\n", config.WebPort)

	db, err := database.InitDB(config.DBPath)
	if err != nil {
		log.Error("初始化数据库失败: %v\n", err)
		return
	}
	defer db.Close()

	web.NewServer(config, db).Start(config.WebPort)
}
