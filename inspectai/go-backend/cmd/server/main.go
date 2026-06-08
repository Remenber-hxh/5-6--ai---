package main

import (
	"bufio"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Server — HTTP 服务上下文
type Server struct {
	store              Store
	storeKind          string // "sqlite" / "mem"
	aiClient           *AIClient
	analyticsClient    *AnalyticsClient
	wework             *WeWorkClient
	weworkBot          *WeWorkBotClient
	storageDir         string
	frontendDir        string
	authToken          string
	supervisorToken    string
	corsAllowedOrigins map[string]bool
}

func main() {
	// 自读 .env，避免依赖 PowerShell 把 Process-level env 传给子进程。
	// 优先级：已存在的真实环境变量 > .env 文件 > 代码默认。
	loadDotEnvIfPresent()
	storageDir := getenv("STORAGE_DIR", "../storage")
	frontendDir := getenv("FRONTEND_DIR", "../frontend")
	aiURL := getenv("AI_SERVICE_URL", "http://127.0.0.1:19100")
	// 管理 AI(DeepSeek)走 ai-service 的 /management/* 内部路由 —— 不开新公网端口。
	// 没单独设的话沿用 AI_SERVICE_URL,跟视觉识别共用同一个进程。
	analyticsURL := getenv("ANALYTICS_SERVICE_URL", aiURL)
	addr := getenv("BACKEND_ADDR", ":18080")
	authToken := getenvWithSecret("INSPECTAI_AUTH_TOKEN", "")
	supervisorToken := getenvWithSecret("INSPECTAI_SUPERVISOR_TOKEN", "")
	weworkClient, err := NewWeWorkClient(WeWorkConfig{
		BaseURL:   getenv("WEWORK_API_BASE_URL", "https://qyapi.weixin.qq.com"),
		CorpID:    getenv("WEWORK_CORP_ID", ""),
		AgentID:   getenv("WEWORK_AGENT_ID", ""),
		AppSecret: getenvWithSecret("WEWORK_APP_SECRET", ""),
	})
	if err != nil {
		log.Printf("WARN: 企业微信配置无效，消息发送已禁用: %v", err)
		weworkClient = NewDisabledWeWorkClient()
	}
	weworkBotClient := NewWeWorkBotClient(getenvWithSecret("WEWORK_BOT_WEBHOOK", ""))
	identitySeed := IdentitySeed{
		Username:    getenv("INSPECTAI_ADMIN_USER", defaultAdminUser),
		Password:    getenvWithSecret("INSPECTAI_ADMIN_PASSWORD", defaultAdminPass),
		DisplayName: getenv("INSPECTAI_ADMIN_NAME", defaultAdminName),
	}
	corsAllowedOrigins := parseAllowedOrigins(
		getenv("CORS_ALLOWED_ORIGINS", ""),
		getenv("WEWORK_TRUSTED_DOMAIN", ""),
	)

	if err := os.MkdirAll(storageDir, 0755); err != nil {
		log.Fatalf("create storage dir: %v", err)
	}

	// 选 store：DB_DRIVER=mysql 优先；否则走 SQLite；都失败回 MemStore
	var store Store
	storeKind := "mem"
	driver := getenv("DB_DRIVER", "sqlite")
	switch driver {
	case "mysql":
		dsn := getenvWithSecret("MYSQL_DSN", "")
		if dsn == "" {
			log.Fatalf("DB_DRIVER=mysql 但 MYSQL_DSN 未设置")
		}
		s, err := NewMySQLStore(dsn)
		if err != nil {
			log.Fatalf("MySQL 初始化失败: %v", err)
		}
		store = s
		storeKind = "mysql"
		log.Printf("MySQL store connected")
	default:
		if dbPath := getenv("SQLITE_PATH", "./inspectai.db"); dbPath != "" {
			fullPath := dbPath
			if !filepathIsAbs(dbPath) {
				fullPath = storageDir + "/" + dbPath
			}
			s, err := NewSQLiteStore(fullPath)
			if err != nil {
				log.Printf("WARN: SQLite init failed (%v), fallback to MemStore", err)
				store = NewMemStore()
			} else {
				store = s
				storeKind = "sqlite"
				log.Printf("SQLite store: %s", fullPath)
			}
		} else {
			store = NewMemStore()
		}
	}
	defer store.Close()
	if err := store.EnsureIdentitySeed(identitySeed); err != nil {
		log.Printf("WARN: identity seed failed: %v", err)
	}
	if err := ensureEngineeringPlanSeeds(store); err != nil {
		log.Printf("WARN: engineering plan seed failed: %v", err)
	}

	server := &Server{
		store:              store,
		storeKind:          storeKind,
		aiClient:           NewAIClient(aiURL),
		analyticsClient:    NewAnalyticsClient(analyticsURL),
		storageDir:         storageDir,
		frontendDir:        frontendDir,
		authToken:          authToken,
		supervisorToken:    supervisorToken,
		wework:             weworkClient,
		weworkBot:          weworkBotClient,
		corsAllowedOrigins: corsAllowedOrigins,
	}
	if err := server.ensureAssetLedgerFromRecords(); err != nil {
		log.Printf("WARN: asset ledger backfill failed: %v", err)
	}
	if err := server.backfillAssetSnapshots(); err != nil {
		log.Printf("WARN: asset snapshot backfill failed: %v", err)
	}
	if removed, err := server.cleanupTmpClassifyDirs(); err != nil {
		log.Printf("WARN: cleanup tmp_classify failed: %v", err)
	} else if removed > 0 {
		log.Printf("Cleaned %d expired tmp_classify dirs", removed)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", server.router)

	log.Printf("InspectAI go-backend listening on %s", addr)
	log.Printf("  AI service URL: %s", aiURL)
	log.Printf("  Storage dir: %s", storageDir)
	log.Printf("  Frontend dir: %s", frontendDir)
	log.Printf("  Store: %s", storeKind)
	if authToken == "" {
		log.Printf("  Auth: local-only no-token mode")
	} else {
		log.Printf("  Auth: token required for /api and /storage")
	}
	if supervisorToken == "" {
		log.Printf("  Supervisor token: not configured (supervisor APIs require local no-token mode)")
	} else {
		log.Printf("  Supervisor token: configured")
	}
	log.Printf("  WeCom message: %v", weworkClient.Enabled())
	log.Printf("  WeCom bot message: %v", weworkBotClient.Enabled())
	log.Printf("  CORS origins: %s", strings.Join(originList(corsAllowedOrigins), ", "))

	srv := &http.Server{
		Addr:         addr,
		Handler:      requestLog(mux),
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 120 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}

func requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

// loadDotEnvIfPresent 在 server.exe 所在目录 / 上级目录 / cwd / cwd/.. 找 .env，逐行 set 进当前进程环境。
// 已存在的变量不覆盖（环境变量优先级高于文件）。
// 用 os.Executable() 而不是只看 cwd，因为 PowerShell ShellExecute 启动时 cwd 不可靠。
func loadDotEnvIfPresent() {
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, ".env"),
			filepath.Join(exeDir, "..", ".env"),
		)
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(cwd, ".env"),
			filepath.Join(cwd, "..", ".env"),
		)
	}
	for _, path := range candidates {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		count := 0
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			// 去掉可能的 UTF-8 BOM（notepad 保存常带）
			line := strings.TrimPrefix(scanner.Text(), "\xef\xbb\xbf")
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			idx := strings.Index(line, "=")
			if idx <= 0 {
				continue
			}
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			val = strings.Trim(val, `"'`)
			if os.Getenv(key) == "" {
				_ = os.Setenv(key, val)
				count++
			}
		}
		f.Close()
		log.Printf("Loaded %d env vars from: %s", count, path)
		return
	}
	log.Printf("WARN: .env not found in any candidate path")
}

// getenvWithSecret 读敏感环境变量，优先支持 <name>_FILE 约定（Docker secret 标准），
// 其次回退普通环境变量，最后用 fallback。
// Why: 生产环境密钥不应明文出现在 env，secret 会挂在 /run/secrets/<name>，
// 由 <NAME>_FILE 指向其路径，进程在启动时读一次即可。
func getenvWithSecret(name, fallback string) string {
	if path := strings.TrimSpace(os.Getenv(name + "_FILE")); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			return strings.TrimSpace(string(data))
		} else {
			log.Printf("WARN: %s_FILE=%s 读取失败: %v，回退到 %s", name, path, err, name)
		}
	}
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func filepathIsAbs(p string) bool {
	if len(p) >= 2 && p[1] == ':' {
		return true
	}
	if len(p) > 0 && (p[0] == '/' || p[0] == '\\') {
		return true
	}
	return false
}

func parseAllowedOrigins(raw, weworkDomain string) map[string]bool {
	origins := map[string]bool{
		"http://127.0.0.1:18080": true,
		"http://localhost:18080": true,
		"http://127.0.0.1:18081": true,
		"http://localhost:18081": true,
		"http://127.0.0.1:18088": true,
		"http://localhost:18088": true,
	}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			origins[strings.TrimRight(item, "/")] = true
		}
	}
	weworkDomain = strings.TrimSpace(weworkDomain)
	if weworkDomain != "" {
		weworkDomain = strings.TrimPrefix(strings.TrimPrefix(weworkDomain, "https://"), "http://")
		origins["https://"+strings.TrimRight(weworkDomain, "/")] = true
	}
	return origins
}

func originList(origins map[string]bool) []string {
	out := make([]string, 0, len(origins))
	for origin := range origins {
		out = append(out, origin)
	}
	sort.Strings(out)
	return out
}
