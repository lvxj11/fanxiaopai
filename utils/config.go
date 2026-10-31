package utils

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Config 存储配置信息
type Config struct {
	LogLevel string // 日志级别：debug, info, warn, error
	WebPort  string // Web 服务器端口号
	DBPath   string // SQLite 数据库文件路径
}

// LoadConfig 加载配置
// 优先级：环境变量 > data/config.ini > 默认值（逐项覆盖，而非整体二选一）
func LoadConfig() (*Config, error) {
	config := &Config{
		LogLevel: "warn",
		WebPort:  "8080",
		DBPath:   "./data/menu.db",
	}

	if err := os.MkdirAll("./data", 0755); err != nil {
		return config, fmt.Errorf("创建数据目录失败: %v", err)
	}

	configPath := "./data/config.ini"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// 默认值就能跑起来，生成配置文件只是方便日后改端口和日志级别，
		// 所以这里不打断启动，写失败也只记一条警告
		if err := GenerateDefaultConfig(config); err != nil {
			GetLogger().Warn("生成默认配置文件失败，已忽略，继续以默认值运行: %v\n", err)
		}
	} else if err := loadConfigFile(config, configPath); err != nil {
		return config, err
	}

	applyEnvOverrides(config)
	return config, nil
}

// loadConfigFile 从 ini 文件读取配置，覆盖已有配置项
func loadConfigFile(config *Config, configPath string) error {
	file, err := os.Open(configPath)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	currentSection := ""

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = strings.ToLower(strings.Trim(line, "[]"))
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.Trim(strings.TrimSpace(parts[1]), "\"'")

		switch currentSection {
		case "server":
			switch key {
			case "log_level":
				config.LogLevel = value
			case "web_port":
				config.WebPort = value
			case "db_path":
				config.DBPath = value
			}
		}
	}

	return scanner.Err()
}

// applyEnvOverrides 用环境变量逐项覆盖配置（最高优先级）
func applyEnvOverrides(config *Config) {
	if logLevel := os.Getenv("LOG_LEVEL"); logLevel != "" {
		config.LogLevel = logLevel
		GetLogger().Debug("使用环境变量覆盖 LogLevel: %s\n", logLevel)
	}
	if webPort := os.Getenv("WEB_PORT"); webPort != "" {
		config.WebPort = webPort
		GetLogger().Debug("使用环境变量覆盖 WebPort: %s\n", webPort)
	}
	if dbPath := os.Getenv("DB_PATH"); dbPath != "" {
		config.DBPath = dbPath
		GetLogger().Debug("使用环境变量覆盖 DBPath: %s\n", dbPath)
	}
}

// GenerateDefaultConfig 生成默认配置文件
func GenerateDefaultConfig(config *Config) error {
	content := `# 饭小排（家常饭排班表）配置文件
# 环境变量 LOG_LEVEL / WEB_PORT / DB_PATH 可逐项覆盖这里的设置

[server]
# 日志级别：debug, info, warn, error
log_level = warn
# Web 服务器端口号
web_port = 8080
# SQLite 数据库路径（容器内保持默认即可，由 docker-compose 挂载 data/ 卷）
db_path = ./data/menu.db
`

	if err := os.MkdirAll("./data", 0755); err != nil {
		return fmt.Errorf("创建数据目录失败: %v", err)
	}
	if err := os.WriteFile("./data/config.ini", []byte(content), 0644); err != nil {
		return fmt.Errorf("生成默认配置文件失败: %v", err)
	}

	GetLogger().Startup("已生成默认配置文件 ./data/config.ini（改端口或日志级别后重启生效）\n")
	return nil
}
