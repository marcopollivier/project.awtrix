BIN := bin/awtrix-hub
FIXTURE := internal/sources/football/testdata/match

.PHONY: build test test-scripts lint-scripts sim run dev mock preview replay vet lint

build:
	go build -o $(BIN) ./cmd/awtrix-hub

test:
	go test ./...

vet:
	go vet ./...

# gofmt + vet + golangci-lint v2 (the same checks as the Go CI)
GOLANGCI_LINT ?= golangci-lint
lint: vet
	@out=$$(gofmt -l .); [ -z "$$out" ] || { echo "Arquivos sem gofmt:"; echo "$$out"; exit 1; }
	$(GOLANGCI_LINT) run ./...

# scripts/*.ax (os apps que rodam no relógio) contra imitações do runtime do AWTRIX NG.
# Precisa do interpretador Berry: git clone https://github.com/berry-lang/berry && make -C berry
BERRY ?= berry
test-scripts:
	@command -v $(BERRY) >/dev/null || { echo "Berry não encontrado; use BERRY=/caminho/para/berry"; exit 1; }
	$(BERRY) scripts/test/run.be

# compila cada scripts/*.ax e confere o cabeçalho (@config antes do código, só o counter é @module)
lint-scripts:
	@command -v $(BERRY) >/dev/null || { echo "Berry não encontrado; use BERRY=/caminho/para/berry"; exit 1; }
	$(BERRY) scripts/test/lint.be

sim:
	go run ./cmd/awtrix-hub sim

run:
	go run ./cmd/awtrix-hub run

# simulador + hub no mesmo processo
dev:
	go run ./cmd/awtrix-hub run --sim

preview:
	go run ./cmd/awtrix-hub preview

# tudo com dados de mentira (sem chaves, sem tocar no estado real)
mock:
	go run ./cmd/awtrix-hub run --mock

# replay de um jogo do Vasco (gols, intervalo, fim) no simulador
replay:
	go run ./cmd/awtrix-hub run --sim --only football --football-fixture $(FIXTURE)
