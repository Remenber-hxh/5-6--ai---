# 智巡 InspectAI 一键工具链(POSIX;Windows 用 Git Bash 或看 scripts/*.ps1)
.PHONY: all build test lint web dist verify clean

all: lint test build web

# ===== 后端 =====
build:
	cd go-backend && go build ./...

test:
	cd go-backend && go test ./...

lint:
	cd go-backend && go vet ./...

# ===== 新版管理后台 =====
web:
	cd admin-web && npx tsc --noEmit

dist:
	cd admin-web && npm run build

# ===== 冒烟验收(需先起本地服务:scripts/start-local.ps1 + admin-web dev) =====
verify:
	python tools/e2e/smoke_asset_crud.py

clean:
	rm -f go-backend/cmd/server/*.go.[0-9]*
