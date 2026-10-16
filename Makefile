BIN := bin/awtrix-hub
FIXTURE := internal/sources/football/testdata/match

.PHONY: build test test-scripts sim run dev mock preview replay vet

build:
	go build -o $(BIN) ./cmd/awtrix-hub

test:
	go test ./...

vet:
	go vet ./...

# scripts/*.ax (os apps que rodam no relógio) contra imitações do runtime do AWTRIX NG.
# Precisa do interpretador Berry: git clone https://github.com/berry-lang/berry && make -C berry
BERRY ?= berry
test-scripts:
	@command -v $(BERRY) >/dev/null || { echo "Berry não encontrado; use BERRY=/caminho/para/berry"; exit 1; }
	$(BERRY) scripts/test/run.be

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
