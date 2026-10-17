---
name: review
description: Code review específico deste projeto (AWTRIX NG, hub em Go + scripts Berry no relógio). Use para revisar o branch atual, um diff ou uma PR do project.awtrix contra as regras da casa — API NG estrita, contrato deploy.Settings ↔ @config, limites do firmware, paridade Go↔Berry, segredos. Complementa a skill genérica code-review, que caça bugs mas não conhece essas regras.
---

# Review do project.awtrix

Revisa uma mudança contra as regras que só valem neste repositório. Os bugs
genéricos de lógica ficam com a `code-review`; aqui o foco é o que quebra o
relógio, desalinha os dois runtimes ou vaza um token.

## 1. Pegar o diff

- Sem argumento: `git diff main...HEAD` (mais `git diff` se houver alteração não commitada).
- Número de PR: `gh pr diff <n>` e `gh pr view <n> --json title,body,files`.
- Um intervalo ou caminho: use-o como veio (`git diff HEAD~3`, `git diff main -- scripts/`).

Liste os arquivos tocados e classifique-os: **Go** (`internal/`, `cmd/`),
**Berry** (`scripts/*.ax`, `scripts/test/`), **simulador web** (`internal/sim/web/`),
**config/segredos** (`config.example.yaml`, `.env.example`, `internal/config/`),
**CI/ferramentas** (`.github/`, `.githooks/`, `.claude/`, `Makefile`).
Leia o arquivo inteiro de cada trecho alterado quando o diff sozinho não basta para julgar.

## 2. Checklist

Passe só pelos blocos que o diff toca. Cada item diz onde conferir.

### API do AWTRIX NG (Go)
- `internal/awtrix/types.go` só tem chaves reais do payload NG: camelCase, tempos
  em milissegundos, `omitempty` onde o NG espera ausência. Chave nova ou renomeada:
  confira na referência citada no topo do arquivo. Uma chave desconhecida faz o
  relógio responder 422.
- Rotas em `internal/awtrix/client.go`/`device.go` seguem as rotas NG
  (`PUT /api/v1/apps/pushed/{name}`, `DELETE /api/v1/apps/{name}`,
  `POST /api/v1/notifications`). Nada de rota ou campo do AWTRIX 3, que foi removido de propósito.
- O simulador imita a mesma rigidez: `internal/sim/server.go` exige
  `application/json` e usa `DisallowUnknownFields`. Payload novo precisa passar
  ali, e `internal/sim/web/matrix.js` precisa ler o nome de campo NG novo.
  Se o diff afrouxa essa rigidez, é achado.

### Contrato deploy ↔ scripts
- Toda chave em `deploy.Settings` (`internal/deploy/deploy.go`) tem a linha
  `# @config <chave>` no script correspondente, e vice-versa quando a chave deveria
  vir do config. `TestScriptsMatchSettings` (`internal/deploy/scripts_test.go`)
  cobre um sentido. O outro sentido (`@config` que ninguém preenche) se confere à mão.
- Segredo vazio não é enviado: chaves de token passam pelo helper `secret(...)`
  dentro de `Settings`, não direto no map.
- `source` nunca aparece em `Settings`, porque fica como o usuário escolheu no relógio.
- `@config` removido ou renomeado apaga o valor do usuário no relógio no próximo
  deploy. Exija que seja intencional e que a PR avise.

### Scripts Berry (`scripts/*.ax`)
- As linhas `# @config` ficam no cabeçalho, antes de qualquer código. Só `counter.ax` tem `# @module`.
- Memória e rede: 8 KB por resposta HTTP e 96 KB para todos os scripts juntos.
  Requisição nova usa as opções `find`/`keep` com um `keep` justo (veja `github.ax`
  e `youtube.ax`) e extrai com `re.search`/`re.matchall`. `json.load` em resposta
  de API externa é achado.
- Regex sobre JSON de terceiros aceita espaço opcional (`": *"`), porque GitHub e
  Google formatam o JSON com espaços. Exceção: a resposta do hub
  (`counter.ax`, `"value":`/`"age":`), que vem compacta do Go.
- `draw()` não monta nada: sem concatenação de string, sem lista nova, sem
  `count()`. As peças e a barra são preparadas em `render()` (`counter.ax`).
- Scope do `store`: o app passa o próprio `store` para a base e a base lê
  `self.st`. O módulo só lê `store` na importação, para `counter.cfg`.
  `store.get` dentro de método da base é achado.
- Apps novos herdam de `counter.Counter`, implementam só `direct(done)` e
  chamam `done(valor)` ou `done(nil, status)`. Sem o status, o backoff
  (400/401/403/429 esperam o intervalo inteiro) não funciona.
- A GitHub REST de repositórios tem ~350 KB, então estrelas só via GraphQL (`github_stars.ax`).

### Paridade Go ↔ Berry
A base `counter.ax` é a irmã de `scheduler.counter` (`internal/scheduler/scheduler.go`).
Pares para comparar:

| Go | Berry |
|---|---|
| `scheduler.counter` (variação do dia, metas, comemoração) | `Counter.got`/`render` em `counter.ax` |
| `internal/render` (`Count`) | `Counter.count`/`tenths` |
| `internal/milestones` | `step`/`pct` e a comemoração em `got` |
| `scheduler.loop` (backoff 1 min → 30 min) | `Counter.got` (backoff até o intervalo; 400/401/403/429 esperam o intervalo inteiro) |
| `internal/api` (`/api/v1/metrics/{key}` → `{"value","age"}`) | `Counter.from_hub` |
| `deploy.Settings` | linhas `# @config` |

Se o diff mexe num lado de um desses pares:
- O outro lado mudou junto, ou a PR explica por que não. "Só faz sentido no
  relógio" é uma justificativa válida, mas precisa estar escrita.
- As chaves de métrica continuam iguais nos dois runtimes: `github_followers`,
  `github_stars`, `youtube_subs`, `instagram_followers`, `linkedin_followers`, `tiktok_followers`
  (`Key:` em `internal/sources/*/`, `super(self).init(store, counter.cfg, "<chave>"` nos `.ax`).
- Se existir a skill `parity` (`.claude/skills/parity/SKILL.md`), siga o roteiro
  dela para essa parte. Se não existir, faça a comparação acima à mão.

### Segredos e dados sensíveis
Vale para diff que toca token, HTTP, config, log, `internal/api`, `internal/sim`, `.github/` ou `.githooks/`:
- Nenhum valor real em arquivo versionado. `.env` e `config.yaml` continuam no `.gitignore`.
  Testdata usa valor falso (`"x"`).
- Token não vai para log (hub ou `print` nos scripts, que aparece em `GET /api/v1/logs`),
  nem para mensagem de erro, nem para o estado salvo em `~/.awtrix-hub/`.
- Workflow novo: `permissions` mínimas, sem `pull_request_target` com checkout do PR, e actions fixadas por SHA.
- Se existir a skill `security-audit`, rode o modo diff dela para esses arquivos.
- Nunca copie o valor de um segredo para o review. Diga só arquivo e linha.

### Testes e idioma
- Mudança de comportamento em Go tem teste em `go test ./...`. Em Berry, tem caso
  em `scripts/test/scripts_test.be`. Mudança em `ng.be` precisa refletir o
  firmware, não acomodar o script.
- Texto para o usuário (CLI, log, README, `@config`/`help`, commit, PR) em
  português. Comentários e identificadores em inglês.
- `README.md`/`CLAUDE.md` atualizados se a mudança altera comando, config ou contrato.

## 3. Rodar o que for barato

Se houver Go no diff: `make lint` (gofmt, vet e golangci-lint com gosec, na versão do CI) e `go test ./...`. Leia a saída inteira, sem `tail`: um achado do lint barra o push. Se houver Berry e um
interpretador (`berry` no PATH ou `$BERRY`): `make test-scripts BERRY=...`. Se
não houver Berry, diga isso no relatório em vez de supor que passa. Falha de
teste é achado bloqueante, com a saída relevante.

## 4. Relatório

Em português, achados ordenados por severidade:

- **Bloqueante:** quebra o relógio (422, estouro de memória, `@config` perdido), vaza segredo, ou deixa os runtimes divergentes sem aviso.
- **Importante:** viola uma regra acima sem quebrar nada ainda, ou falta teste.
- **Sugestão:** limpeza, nome, texto.

Cada achado traz `arquivo:linha`, o que está errado, o cenário concreto (o que
acontece no relógio, no simulador ou no hub) e a correção. Termine com uma linha
de veredito (pode ir, pode ir com ajustes, ou não pode ir) e com a lista do que
foi rodado no passo 3.

Não poste nada na PR por conta própria. Se o usuário pedir, use
`gh pr review <n> --comment --body-file <arquivo>`, ou `--request-changes`
quando houver achado bloqueante.
