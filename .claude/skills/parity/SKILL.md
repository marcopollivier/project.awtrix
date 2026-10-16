---
name: parity
description: Verifica se o hub em Go e os scripts Berry do relógio implementam a mesma coisa. Use quando uma mudança tocar scripts/*.ax ou internal/scheduler, render, milestones, sources, api ou deploy, quando o usuário pedir para checar paridade Go↔Berry, ou como parte de um code review.
---

# Paridade Go ↔ Berry

O projeto tem dois runtimes que mostram os mesmos contadores: o hub (Go) e os scripts no relógio (Berry). Uma mudança num lado sem o equivalente no outro faz o relógio e o simulador divergirem. Esta skill acha essas diferenças.

## 1. O que mudou

Por padrão, compare o branch com `main`:

```sh
git diff --name-only main...HEAD
git diff main...HEAD -- scripts/ internal/
```

Se o usuário passar um arquivo, um commit ou um número de PR (`gh pr diff <n>`), use isso.

## 2. Mapa de correspondência

Para cada arquivo alterado, abra o outro lado e leia os dois. Não confie só no diff.

| Go (hub) | Berry (relógio) | O que precisa bater |
|---|---|---|
| `internal/render/render.go` `Count`, `Delta` | `scripts/counter.ax` `count()`, `tenths()`, pedaços de `render()` | formato compacto (793, 12.3K, 1.2M), sinal do delta, espaço antes do delta |
| `internal/milestones` `Reached`, `Progress`, `Check` | `counter.ax` `got()` (campo `c`) e `render()` (`pct`) | meta atingida, % da barra, primeira leitura não comemora, queda e volta não comemoram de novo |
| `internal/scheduler/scheduler.go` `counter()` | `counter.ax` `got()` + `render()` | virada do dia (`DayStart` ↔ `s`), cor cinza quando velho, texto e som da festa (`LABEL 800!`, RTTTL), cor da barra e trilho `#202020` |
| `scheduler.loop` (backoff) | `counter.ax` `got()` (`wait`) | 1, 2, 4… min com teto; ver divergências conhecidas abaixo |
| `internal/sources/*` `Key:`, `Label:` | `super(self).init(store, counter.cfg, "<key>", "<LABEL>")` em cada `scripts/<app>.ax` | mesma chave e mesmo rótulo |
| `internal/sources/*` (URL, campos lidos) | `direct()` de cada `scripts/<app>.ax` | mesma API, mesmo campo (ex.: `followers`, `subscriberCount`, `followers_count`), mesmo tratamento de token |
| `internal/api` `GET /api/v1/metrics/{key}` → `{"value":N,"age":S}` | `counter.ax` `from_hub()` | formato da resposta, idade máxima (`stale_s`) |
| `internal/deploy` `Settings` | linhas `# @config` de cada script | toda chave enviada existe no script; segredo vazio não é enviado; `source` nunca é enviado |
| `internal/config` (padrões) | `default=` das linhas `# @config` | mesmos valores padrão |

Lembre das restrições do firmware ao propor o equivalente em Berry: 8 KB por resposta HTTP, 96 KB de memória, regex (`re.search`) e `find`/`keep` em vez de `json.load`, nada construído em `draw()`.

## 3. Checagem determinística

`testdata/parity.json` guarda vetores que os dois lados testam:

- `keys`: chaves das métricas (`internal/sources/parity_test.go` compara com `Key:` do Go e `init(...)` dos `.ax`);
- `count`, `delta`: formatação (`internal/render/parity_test.go`);
- `progress`, `milestones`: barra e festas (`internal/milestones/parity_test.go`);
- `known_divergences`: diferenças conhecidas e aceitas, testadas para continuar exatas.

O lado Berry lê o mesmo arquivo em `scripts/test/scripts_test.be`.

Rode os dois:

```sh
make test
make test-scripts BERRY=/caminho/para/berry
```

Sem Berry instalado, diga isso no relatório em vez de declarar que passou (`git clone https://github.com/berry-lang/berry && make -C berry`).

Se a mudança cria ou altera um comportamento coberto por um vetor, proponha o caso novo em `parity.json`. Um caso só entra se os dois lados o implementam. Uma diferença real vai para `known_divergences`, com os valores de cada lado, ou é corrigida.

## 4. Divergências já conhecidas

Não reporte estas como novas, mas aponte se a mudança as piorar:

- **Arredondamento na metade:** `render.Count` usa `%.1f` sobre float64 e o Berry arredonda o inteiro para cima. 11250 dá `11.2K` no Go e `11.3K` no Berry.
- **Backoff:** o Go dobra até 30 min e limita ao próximo horário da fonte, sem olhar o status. O Berry dobra até o intervalo do app e espera o intervalo inteiro em 400/401/403/429.
- **Meta em lista:** o Go aceita `milestones.list`; o Berry só tem `step`.
- **Futebol** só existe no hub, de propósito.

## 5. Relatório

Responda em português, curto, nesta forma:

```
Paridade Go ↔ Berry — <branch ou PR>

Divergências
- [alta|média|baixa] <comportamento>: Go faz X (`arquivo.go:linha`), Berry faz Y (`script.ax:linha`). Efeito no relógio: … Correção sugerida: …

Faltando no outro lado
- <mudança em um runtime sem o equivalente no outro>

Vetores sugeridos para testdata/parity.json
- <caso>

Testes: make test <ok|falhou>; make test-scripts <ok|falhou|não rodou: motivo>
```

Se não houver divergência, diga isso numa linha e liste o que foi comparado.
