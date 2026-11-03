package web

import (
	"compress/gzip"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"fanxiaopai/ai"
	"fanxiaopai/database"
	"fanxiaopai/generator"
	"fanxiaopai/utils"
)

//go:embed templates/*
var templateFiles embed.FS

//go:embed static/*
var staticFiles embed.FS

// 设置项主键
const (
	settingMealSlots = "meal_slots"
	settingAvoidTags = "avoid_tags"
)

// defaultMealSlots 默认排餐：工作日早晚，周末三餐
const defaultMealSlots = `{"0":["breakfast","dinner"],"1":["breakfast","dinner"],"2":["breakfast","dinner"],"3":["breakfast","dinner"],"4":["breakfast","dinner"],"5":["breakfast","lunch","dinner"],"6":["breakfast","lunch","dinner"]}`

// Server Web 服务器
type Server struct {
	config    *utils.Config
	db        *database.Database
	generator generator.Generator
	ai        ai.Provider
}

// NewServer 创建服务器实例
func NewServer(config *utils.Config, db *database.Database) *Server {
	return &Server{
		config:    config,
		db:        db,
		generator: generator.NewLocalWeighted(nil),
		ai:        ai.NewNoopProvider(),
	}
}

// Handler 返回路由入口
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.redirectToMenu)
	mux.HandleFunc("/menu", s.showMenu)
	mux.HandleFunc("/dishes", s.showDishes)
	mux.HandleFunc("/shopping", s.showShopping)
	mux.HandleFunc("/settings", s.showSettings)

	mux.HandleFunc("/api/menu", s.apiMenu)
	mux.HandleFunc("/api/menu/generate", s.apiGenerateMenu)
	mux.HandleFunc("/api/menu/copy", s.apiCopyMenu)
	mux.HandleFunc("/api/menu/item/reroll", s.apiRerollItem)
	mux.HandleFunc("/api/menu/item/replace", s.apiReplaceItem)
	mux.HandleFunc("/api/menu/item/lock", s.apiLockItem)
	mux.HandleFunc("/api/shopping", s.apiShopping)
	mux.HandleFunc("/api/shopping/checked", s.apiShoppingChecked)
	mux.HandleFunc("/api/dishes", s.apiDishes)
	mux.HandleFunc("/api/dishes/", s.apiDish)
	mux.HandleFunc("/api/settings/meal-slots", s.apiMealSlots)
	mux.HandleFunc("/api/settings/avoid-tags", s.apiAvoidTags)
	mux.HandleFunc("/api/ai/suggest", s.apiAISuggest)
	mux.HandleFunc("/api/health", s.apiHealth)
	mux.HandleFunc("/favicon.svg", s.serveFavicon)
	mux.HandleFunc("/export", s.exportSnapshot)

	// 静态资源全部编译进二进制，随程序分发，断网也能显示
	mux.Handle("/static/", s.serveStatic())

	return gzipMiddleware(mux)
}

// Start 启动服务器
func (s *Server) Start(port string) {
	log := utils.GetLogger()
	log.Startup("服务器启动在端口 %s\n", port)

	// 启动信息打完就不再往控制台刷日志
	log.SetConsoleOutput(false)

	if err := http.ListenAndServe(":"+port, s.Handler()); err != nil {
		log.SetConsoleOutput(true)
		log.Error("启动服务器失败: %v\n", err)
	}
}

// ---------- 页面 ----------

func (s *Server) redirectToMenu(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/menu", http.StatusFound)
}

func (s *Server) showMenu(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, "排班表", "menu", "menu.html")
}

func (s *Server) showDishes(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, "菜品库", "dishes", "dishes.html")
}

func (s *Server) showShopping(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, "采购清单", "shopping", "shopping.html")
}

func (s *Server) showSettings(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, "设置", "settings", "settings.html")
}

// renderPage 渲染页面骨架 + 内容模板
func (s *Server) renderPage(w http.ResponseWriter, title, currentPage, contentTemplate string) {
	tmpl, err := template.ParseFS(templateFiles, "templates/base.html", "templates/"+contentTemplate)
	if err != nil {
		http.Error(w, "解析模板失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	data := struct {
		Title       string
		CurrentPage string
		Version     string
		Today       string
	}{
		Title:       title,
		CurrentPage: currentPage,
		Version:     utils.Version,
		Today:       time.Now().Format("2006-01-02"),
	}

	if err := tmpl.Execute(w, data); err != nil {
		utils.GetLogger().Error("渲染页面失败: %v\n", err)
	}
}

// serveStatic 静态资源，Service Worker 单独加一个响应头
func (s *Server) serveStatic() http.Handler {
	files := http.FileServer(http.FS(staticFiles))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// sw.js 放在 /static/ 下，默认 scope 只有 /static/，管不到 /menu。
		// 要靠这个头把 scope 提到根，否则离线缓存对页面无效
		if r.URL.Path == "/static/sw.js" {
			w.Header().Set("Service-Worker-Allowed", "/")
			w.Header().Set("Cache-Control", "no-cache")
		}
		// FileServer 不认 .webmanifest 这个扩展名，会按 text/plain 发，浏览器不认
		if r.URL.Path == "/static/manifest.webmanifest" {
			w.Header().Set("Content-Type", "application/manifest+json")
		}
		files.ServeHTTP(w, r)
	})
}

// serveFavicon 返回站点图标
func (s *Server) serveFavicon(w http.ResponseWriter, r *http.Request) {
	data, err := staticFiles.ReadFile("static/favicon.svg")
	if err != nil {
		http.Error(w, "图标不存在", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}

// ---------- 通用工具 ----------

// weekParam 取周参数，缺省为今天所在的那一周（周一）
func weekParam(r *http.Request) string {
	week := r.URL.Query().Get("week")
	if week != "" && isValidDate(week) {
		return week
	}
	return mondayOf(time.Now())
}

// mondayOf 取某天所在周的周一（day_index 0 对应周一，与生成器一致）
func mondayOf(t time.Time) string {
	offset := int(t.Weekday()) - 1
	if offset < 0 {
		offset = 6
	}
	return t.AddDate(0, 0, -offset).Format("2006-01-02")
}

func isValidDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// writeJSON 输出 JSON
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		utils.GetLogger().Error("输出 JSON 失败: %v\n", err)
	}
}

// writeError 输出错误信息
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

// readJSON 解析请求体，同时拒绝明显过大的请求
func readJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	return dec.Decode(dst)
}

// apiError 统一的错误处理：区分预期错误和内部错误
func (s *Server) apiError(w http.ResponseWriter, err error, fallback string) {
	if err == nil {
		return
	}
	if err == generator.ErrNoDishes || err == ai.ErrNotConfigured {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	utils.GetLogger().Error("%s: %v\n", fallback, err)
	writeError(w, http.StatusInternalServerError, fallback)
}

// requirePOST 只允许 POST
func requirePOST(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "只支持 POST")
		return false
	}
	return true
}

// apiHealth 健康检查
func (s *Server) apiHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": utils.Version,
	})
}

// apiAISuggest AI 换新鲜的入口。v1 未接入任何服务，明确返回未配置
func (s *Server) apiAISuggest(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	var req ai.SuggestRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式有误")
		return
	}
	if req.Count <= 0 {
		req.Count = 3
	}

	dishes, err := s.ai.Suggest(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusNotImplemented, fmt.Sprintf("AI 换新鲜暂不可用：%v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dishes": dishes})
}

// gzipResponseWriter 把响应体接到 gzip.Writer 上
type gzipResponseWriter struct {
	http.ResponseWriter
	writer *gzip.Writer
}

func (g gzipResponseWriter) Write(b []byte) (int, error) {
	return g.writer.Write(b)
}

// gzipMiddleware 压缩响应体，CSS/JS 与菜单 JSON 都能省不少流量
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		zw := gzip.NewWriter(w)
		defer zw.Close()

		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		w.Header().Del("Content-Length")

		next.ServeHTTP(gzipResponseWriter{ResponseWriter: w, writer: zw}, r)
	})
}
