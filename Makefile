BIN := bin/awtrix-hub
FIXTURE := internal/sources/football/testdata/match

.PHONY: build test sim run dev preview replay vet

build:
	go build -o $(BIN) ./cmd/awtrix-hub

test:
	go test ./...

vet:
	go vet ./...

sim:
	go run ./cmd/awtrix-hub sim

run:
	go run ./cmd/awtrix-hub run

# simulador + hub no mesmo processo
dev:
	go run ./cmd/awtrix-hub run --sim

preview:
	go run ./cmd/awtrix-hub preview

# replay de um jogo do Vasco (gols, intervalo, fim) no simulador
replay:
	go run ./cmd/awtrix-hub run --sim --only football --football-fixture $(FIXTURE)
