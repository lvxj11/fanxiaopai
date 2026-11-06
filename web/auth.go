package web

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"fanxiaopai/utils"
)

const (
	sessionCookieName = "fxp_session"
	authSettingKey    = "auth"
)

// credentials 作为一整个 JSON blob 存在 settings 表的 auth 键里。
// 单条原子写入既是「是否已初始化」的判定，也把会话签名密钥一起持久化，
// 重启后已签发的会话不会失效（无需额外表或列）。
type credentials struct {
	Username   string `json:"username"`
	Salt       string `json:"salt"` // base64 随机盐
	Hash       string `json:"hash"` // base64 PBKDF2 派生密钥
	Iterations int    `json:"iterations"`
	Secret     string `json:"secret"` // base64 会话签名密钥
}

func (s *Server) hasCredentials() bool {
	return s.db.GetSetting(authSettingKey, "") != ""
}

func (s *Server) loadCredentials() (*credentials, error) {
	raw := s.db.GetSetting(authSettingKey, "")
	if raw == "" {
		return nil, nil
	}
	var c credentials
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func derivePassword(password string, salt []byte, iterations int) ([]byte, error) {
	return pbkdf2.Key(sha256.New, password, salt, iterations, 32)
}

func (s *Server) verifyCredentials(user, pass string) bool {
	creds, err := s.loadCredentials()
	if err != nil || creds == nil || creds.Username != user {
		return false
	}
	salt, err := base64.RawURLEncoding.DecodeString(creds.Salt)
	if err != nil {
		return false
	}
	want, err := base64.RawURLEncoding.DecodeString(creds.Hash)
	if err != nil {
		return false
	}
	got, err := derivePassword(pass, salt, creds.Iterations)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// issueSession 签发一个自带过期时间、HMAC 签名的会话 Cookie。
func (s *Server) issueSession(w http.ResponseWriter, username string, creds *credentials) {
	secret, _ := base64.RawURLEncoding.DecodeString(creds.Secret)
	exp := time.Now().Add(s.config.SessionTTL).Unix()
	payload := username + "|" + strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	token := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sig

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.config.Domain != "",
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(exp, 0),
		MaxAge:   int(s.config.SessionTTL.Seconds()),
	})
}

func (s *Server) validateSession(r *http.Request) bool {
	creds, err := s.loadCredentials()
	if err != nil || creds == nil {
		return false
	}
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	parts := strings.SplitN(c.Value, ".", 2)
	if len(parts) != 2 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	secret, _ := base64.RawURLEncoding.DecodeString(creds.Secret)
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	if !hmac.Equal(mac.Sum(nil), sig) {
		return false
	}
	fields := strings.SplitN(string(payload), "|", 2)
	if len(fields) != 2 || fields[0] != creds.Username {
		return false
	}
	exp, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	return true
}

func (s *Server) clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.config.Domain != "",
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}

// ---------- 中间件 ----------

func isPublicPath(path string) bool {
	switch path {
	case "/api/health", "/favicon.svg", "/login", "/api/login", "/setup", "/api/setup", "/api/logout":
		return true
	}
	return strings.HasPrefix(path, "/static/")
}

func isAPIPath(path string) bool {
	return strings.HasPrefix(path, "/api/")
}

func isMutating(r *http.Request, path string) bool {
	if !isAPIPath(path) {
		return false
	}
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
		return true
	}
	return false
}

// checkCSRF 最小集：变更类 /api/* 必须带 application/json，且来源在白名单内。
// SameSite=Lax 是主要防线，这里是纵深；curl 等无 Origin/Referer 的脚本放行。
func (s *Server) checkCSRF(r *http.Request) bool {
	ct := strings.ToLower(r.Header.Get("Content-Type"))
	if !strings.HasPrefix(ct, "application/json") {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if u.Host == r.Host {
		return true
	}
	if s.config.Domain != "" && u.Hostname() == s.config.Domain {
		return true
	}
	return false
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		if isMutating(r, path) && !s.checkCSRF(r) {
			writeError(w, http.StatusForbidden, "请求来源不被允许")
			return
		}

		if isPublicPath(path) {
			next.ServeHTTP(w, r)
			return
		}

		if !s.hasCredentials() {
			if isAPIPath(path) {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "尚未初始化", "setup": true})
			} else {
				http.Redirect(w, r, "/setup", http.StatusFound)
			}
			return
		}

		if !s.validateSession(r) {
			if isAPIPath(path) {
				writeError(w, http.StatusUnauthorized, "未登录或会话已过期")
			} else {
				http.Redirect(w, r, "/login", http.StatusFound)
			}
			return
		}

		next.ServeHTTP(w, r)
	})
}

// ---------- 页面 ----------

// renderStandalone 渲染完整独立页面（登录/设置），不走 base.html/renderPage，
// 因此不带 dock、主题脚本、SW 注册。
func (s *Server) renderStandalone(w http.ResponseWriter, name string, data any) {
	tmpl, err := template.ParseFS(templateFiles, name)
	if err != nil {
		http.Error(w, "解析模板失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		utils.GetLogger().Error("渲染页面失败: %v\n", err)
	}
}

func (s *Server) showLogin(w http.ResponseWriter, r *http.Request) {
	if !s.hasCredentials() {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}
	if s.validateSession(r) {
		http.Redirect(w, r, "/menu", http.StatusFound)
		return
	}
	s.renderStandalone(w, "templates/login.html", map[string]any{"Version": utils.Version})
}

func (s *Server) showSetup(w http.ResponseWriter, r *http.Request) {
	if s.hasCredentials() {
		http.NotFound(w, r) // 初始化只能做一次
		return
	}
	s.renderStandalone(w, "templates/setup.html", map[string]any{"Version": utils.Version})
}

// ---------- 接口 ----------

func (s *Server) apiLogin(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	if !s.hasCredentials() {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "尚未初始化", "setup": true})
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式有误")
		return
	}
	if !s.verifyCredentials(body.Username, body.Password) {
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}

	// 【二次验证预留接缝】将来选定方式（TOTP / 短信 / Passkey / …）后，
	// 在此处插入校验 + 短时签名 pending cookie，校验通过再签发正式会话。
	// 当前为空操作，不引入任何 2FA 专用表/字段/配置项/接口/UI。
	creds, err := s.loadCredentials()
	if err != nil || creds == nil {
		writeError(w, http.StatusInternalServerError, "读取凭据失败")
		return
	}
	s.issueSession(w, body.Username, creds)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) apiLogout(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	s.clearSession(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// apiSetup 首次初始化：可选导入内置菜，再写入凭据并直接登录。
// 必须先导入种子、成功后再写凭据——导入失败时不落凭据，用户仍停在 /setup 可重试。
func (s *Server) apiSetup(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	s.setupMu.Lock()
	defer s.setupMu.Unlock()

	if s.hasCredentials() {
		http.NotFound(w, r) // 初始化只能做一次
		return
	}

	var body struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		ImportSeed bool   `json:"import_seed"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式有误")
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	if body.Username == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "用户名和密码不能为空")
		return
	}

	imported := 0
	if body.ImportSeed {
		n, err := s.db.ImportSeedDishes()
		if err != nil {
			utils.GetLogger().Error("首次设置导入内置菜品失败: %v\n", err)
			writeError(w, http.StatusInternalServerError, "导入内置菜品失败，请重试")
			return
		}
		imported = n
	}

	const iterations = 120000
	salt := make([]byte, 16)
	secret := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		writeError(w, http.StatusInternalServerError, "生成凭据失败")
		return
	}
	if _, err := rand.Read(secret); err != nil {
		writeError(w, http.StatusInternalServerError, "生成凭据失败")
		return
	}
	hash, err := derivePassword(body.Password, salt, iterations)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成凭据失败")
		return
	}
	creds := &credentials{
		Username:   body.Username,
		Salt:       base64.RawURLEncoding.EncodeToString(salt),
		Hash:       base64.RawURLEncoding.EncodeToString(hash),
		Iterations: iterations,
		Secret:     base64.RawURLEncoding.EncodeToString(secret),
	}
	blob, err := json.Marshal(creds)
	if err != nil || s.db.SetSetting(authSettingKey, string(blob)) != nil {
		writeError(w, http.StatusInternalServerError, "保存凭据失败")
		return
	}

	s.issueSession(w, body.Username, creds)
	utils.GetLogger().Startup("首次设置完成：用户 %s，导入内置菜 %d 道\n", body.Username, imported)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "imported": imported})
}
