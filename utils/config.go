package utils

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config 存储配置信息
type Config struct {
	LogLevel   string        // 日志级别：debug, info, warn, error
	WebHost    string        // 监听地址，0.0.0.0 表示全网卡
	WebPort    string        // Web 服务器端口号
	Domain     string        // 公网域名（不含协议），留空 = 纯局域网/本机
	SessionTTL time.Duration // 登录会话有效期
	DBPath     string        // SQLite 数据库文件路径
}

// configDir 配置目录：CONFIG_DIR 环境变量优先，默认 ./config。
// 必须在计算 config.ini 路径之前读取，否则环境变量不生效。
func configDir() string {
	if dir := os.Getenv("CONFIG_DIR"); dir != "" {
		return dir
	}
	return "./config"
}

// LoadConfig 加载配置
// 优先级：环境变量 > config/config.ini > 默认值（逐项覆盖，而非整体二选一）
func LoadConfig() (*Config, error) {
	config := &Config{
		LogLevel:   "warn",
		WebHost:    "0.0.0.0",
		WebPort:    "8080",
		Domain:     "",
		SessionTTL: 168 * time.Hour,
		DBPath:     "./data/menu.db",
	}

	// 数据目录给数据库和日志用，配置单独放 config/，不混在一起
	if err := os.MkdirAll("./data", 0755); err != nil {
		return config, fmt.Errorf("创建数据目录失败: %v", err)
	}

	configPath := filepath.Join(configDir(), "config.ini")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// 默认值就能跑起来，生成配置文件只是方便日后改，
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
			case "web_host":
				config.WebHost = value
			case "web_port":
				config.WebPort = value
			case "domain":
				config.Domain = value
			case "session_ttl":
				if hours, err := strconv.Atoi(value); err == nil && hours > 0 {
					config.SessionTTL = time.Duration(hours) * time.Hour
				}
			case "db_path":
				config.DBPath = value
			}
		}
	}

	return scanner.Err()
}

// applyEnvOverrides 用环境变量逐项覆盖配置（最高优先级）
// 注意：CONFIG_DIR 只影响配置文件所在目录，由 configDir() 读取，不覆盖这里任何字段。
func applyEnvOverrides(config *Config) {
	if logLevel := os.Getenv("LOG_LEVEL"); logLevel != "" {
		config.LogLevel = logLevel
		GetLogger().Debug("使用环境变量覆盖 LogLevel: %s\n", logLevel)
	}
	if webHost := os.Getenv("WEB_HOST"); webHost != "" {
		config.WebHost = webHost
		GetLogger().Debug("使用环境变量覆盖 WebHost: %s\n", webHost)
	}
	if webPort := os.Getenv("WEB_PORT"); webPort != "" {
		config.WebPort = webPort
		GetLogger().Debug("使用环境变量覆盖 WebPort: %s\n", webPort)
	}
	if domain := os.Getenv("DOMAIN"); domain != "" {
		config.Domain = domain
		GetLogger().Debug("使用环境变量覆盖 Domain: %s\n", domain)
	}
	if ttl := os.Getenv("SESSION_TTL"); ttl != "" {
		if hours, err := strconv.Atoi(ttl); err == nil && hours > 0 {
			config.SessionTTL = time.Duration(hours) * time.Hour
			GetLogger().Debug("使用环境变量覆盖 SessionTTL: %s 小时\n", ttl)
		}
	}
	if dbPath := os.Getenv("DB_PATH"); dbPath != "" {
		config.DBPath = dbPath
		GetLogger().Debug("使用环境变量覆盖 DBPath: %s\n", dbPath)
	}
}

// GenerateDefaultConfig 生成默认配置文件到 config/config.ini
func GenerateDefaultConfig(config *Config) error {
	content := `# 饭小排（家常饭排班表）配置文件
# 环境变量逐项覆盖这里：LOG_LEVEL / WEB_HOST / WEB_PORT / DOMAIN / SESSION_TTL / DB_PATH / CONFIG_DIR

[server]
# 日志级别：debug, info, warn, error
log_level = warn
# 监听地址，0.0.0.0 表示全网卡
web_host = 0.0.0.0
# Web 服务器端口号
web_port = 8080
# 公网域名（不含协议）。留空 = 局域网/本机，登录 Cookie 不强制 Secure；
# 填写后 Cookie 加 Secure，并把 https://<domain> 加入来源白名单。
domain =
# 登录会话有效期（小时）
session_ttl = 168
# SQLite 数据库路径（容器内保持默认即可，由 docker-compose 挂载 data/ 卷）
db_path = ./data/menu.db
`

	dir := configDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建配置目录失败: %v", err)
	}
	path := filepath.Join(dir, "config.ini")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("生成默认配置文件失败: %v", err)
	}

	GetLogger().Startup("已生成默认配置文件 %s（改端口或日志级别后重启生效）\n", path)
	return nil
}
