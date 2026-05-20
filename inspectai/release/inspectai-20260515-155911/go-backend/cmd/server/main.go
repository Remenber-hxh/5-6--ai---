package main

import (
	"bufio"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Server — HTTP 服务上下文
type Server struct {
	store       Store
	storeKind   string // "sqlite" / "mem"
	aiClient    *AIClient
	storageDir  string
	frontendDir string
	authToken   string
}

func main() {
	// 自读 .env，避免依赖 PowerShell 把 Process-level env 传给子进程。
	// 优先级：已存在的真实环境变量 > .env 文件 > 代码默认。
	loadDotEnvIfPresent()
	storageDir := getenv("STORAGE_DIR", "../storage")
	frontendDir := getenv("FRONTEND_DIR", "../frontend")
	aiURL := getenv("AI_SERVICE_URL", "http://127.0.0.1:19100")
	addr := getenv("BACKEND_ADDR", ":18080")
	authToken := getenv("INSPECTAI_AUTH_TOKEN", "")

	if err := os.MkdirAll(storageDir, 0755); err != nil {
		log.Fatalf("create storage dir: %v", err)
	}

	// 选 store：DB_DRIVER=mysql 优先；否则走 SQLite；都失败回 MemStore
	var store Store
	storeKind := "mem"
	driver := getenv("DB_DRIVER", "sqlite")
	switch driver {
	case "mysql":
		dsn := os.Getenv("MYSQL_DSN")
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

	server := &Server{
		store:       store,
		storeKind:   storeKind,
		aiClient:    NewAIClient(aiURL),
		storageDir:  storageDir,
		frontendDir: frontendDir,
		authToken:   authToken,
	}
	if err := server.ensureAssetLedgerFromRecords(); err != nil {
		log.Printf("WARN: asset ledger backfill failed: %v", err)
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
		log.Printf("  Auth: disabled (local mode)")
	} else {
		log.Printf("  Auth: token required for /api and /storage")
	}

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

func filepathIsAbs(p string) bool {
	if len(p) >= 2 && p[1] == ':' {
		return true
	}
	if len(p) > 0 && (p[0] == '/' || p[0] == '\\') {
		return true
	}
	return false
}
