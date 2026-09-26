VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X mihomo-king/internal/cli.Version=$(VERSION)
PREFIX  ?= $(HOME)/.local

.PHONY: build install completions test clean download-release

build:
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o mihomo-king .

# 下载指定版本到当前目录（需要已登录的 gh）：make download-release RELEASE=v1.2
download-release:
	@test -n "$(RELEASE)" || { echo "用法：make download-release RELEASE=v1.2" >&2; exit 1; }
	gh release download "$(RELEASE)" --repo jokergaming/mihomo-king --pattern 'mihomo-king-linux-amd64.tar.gz' --dir .

install: build completions
	install -Dm755 mihomo-king $(PREFIX)/bin/mihomo-king

# Completion scripts query the installed binary at completion time, so they
# only need regenerating when commands or flags change.
completions: build
	mkdir -p $(PREFIX)/share/bash-completion/completions $(PREFIX)/share/zsh/site-functions
	./mihomo-king completion bash > $(PREFIX)/share/bash-completion/completions/mihomo-king
	./mihomo-king completion zsh > $(PREFIX)/share/zsh/site-functions/_mihomo-king

test:
	go vet ./...
	go test ./...

clean:
	rm -f mihomo-king
