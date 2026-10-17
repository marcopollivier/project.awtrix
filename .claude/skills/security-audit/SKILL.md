---
name: security-audit
description: Análise de segurança focada nos segredos deste projeto (GITHUB_TOKEN, YOUTUBE_API_KEY, INSTAGRAM_TOKEN, AWTRIX_USER/PASS) nos dois runtimes, o hub em Go e os scripts Berry do relógio. Use antes de abrir uma PR que mexa em config, .env, deploy, sources, scripts/*.ax, endpoints HTTP, logs, arquivos em ~/.awtrix-hub ou workflows de CI, quando pedirem "auditoria de segurança", "isso vaza token?" ou uma revisão de segurança do repo inteiro. Argumento opcional "full" audita o repositório todo; sem argumento, só o diff do branch contra main.
---

# Auditoria de segurança

O repositório é **público**. O risco principal é um segredo sair do `.env`, do relógio ou de `~/.awtrix-hub/` e ir parar num commit, num log, numa URL ou numa resposta HTTP sem autenticação. Esta skill segue cada segredo de ponta a ponta e procura por onde ele escapa.

## Regras invioláveis

- **Nunca leia `.env`** nem `~/.awtrix-hub/instagram_token.json`, `tiktok_token.json` ou `tiktok_login_state`. Para saber quais segredos existem, use `.env.example` e `internal/config/config.go`.
- **Nunca imprima o valor de um segredo**, nem parcial. Ferramentas rodam com `--redact`. Um achado diz *onde* está e *qual* segredo é, nunca o valor.
- Não chame `GET /api/v1/apps/instagram/data` nem `GET /api/v1/logs` no relógio real só para "conferir": as duas respostas podem conter o token. A análise é feita no código.
- Esta skill **relata**, não corrige. Proponha a correção em cada achado. Só altere código se o usuário pedir.

## Escopo

- Sem argumento: `git diff main...HEAD` mais os arquivos não commitados (`git status`). Leia cada arquivo tocado **inteiro**, além das funções que chamam ou são chamadas pelo trecho alterado. Um vazamento costuma estar a um salto do diff.
- `full`: todas as superfícies abaixo, o histórico do git e os workflows.

## 1. Ferramentas automáticas (rode primeiro)

```sh
make secrets 2>/dev/null || go run github.com/zricethezav/gitleaks/v8@v8.30.1 detect --redact --no-banner --source .
go run github.com/zricethezav/gitleaks/v8@v8.30.1 detect --redact --no-banner --no-git --source .   # arquivos não commitados
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
make lint                                                                                          # golangci-lint na versão do CI, com gosec
git ls-files | grep -E '(^|/)\.env$|config\.yaml$|state\.json$|instagram_token\.json$'                # não pode imprimir nada
git check-ignore -q .env && git check-ignore -q config.yaml && echo "ok: .env e config.yaml ignorados"
```

O gitleaks com `.gitleaks.toml` na raiz usa as regras do projeto. Sem ele, valem as regras padrão. Um achado do `gosec` no `make lint` (ex.: G117, segredo serializado; G101, credencial no código) é achado desta auditoria: o hook e o CI barram o push. Persistir um segredo de propósito exige um tipo próprio só com o necessário, arquivo `0o600` e `//nolint:gosec // <motivo>` na linha. Tipos que carregam segredo em memória não serializam (`json:"-"`) e se mascaram em `fmt`/`slog` (`String`, `GoString`, `LogValue`), como `tiktok.Token`. No `govulncheck`, separe o que o código **chama** (achado real) do que só está no grafo de dependências (informativo). Vulnerabilidade da biblioteca padrão se resolve subindo a versão do Go no `go.mod`.

## 2. Mapa dos segredos (siga cada um)

| Segredo | Entra | Atravessa | Sai para |
|---|---|---|---|
| `GITHUB_TOKEN` | `config.Load` → `Sources.GitHub.Token` | `deploy.Settings` (`secret(...)` em `github` e `github_stars`) | header `Authorization: Bearer` em `sources/github.(*Source).get`; `@config token` de `scripts/github.ax` e `scripts/github_stars.ax` (header no `http.post` do GraphQL) |
| `YOUTUBE_API_KEY` | `Sources.YouTube.APIKey` | `deploy.Settings` → `api_key` | **query string** `key=` em `sources/youtube.(*Source).get` e em `scripts/youtube.ax` |
| `INSTAGRAM_TOKEN` | `Sources.Instagram.Token` (só semente) | `instagram.(*Source).token` → `~/.awtrix-hub/instagram_token.json`; `deploy.Settings` → `token` | **query string** `access_token=` em `Fetch` e no refresh; no relógio, `store` chave `tok` em `scripts/instagram.ax`, visível em `GET /api/v1/apps/instagram/data` |
| `TIKTOK_CLIENT_KEY`/`TIKTOK_CLIENT_SECRET` e refresh token | `Sources.TikTok.ClientKey/ClientSecret`; o refresh token vem de `awtrix-hub tiktok login` (nunca do `.env`) | `deploy.Settings` → `client_key`/`client_secret` (nunca `refresh_token`); `tiktok.(*Source)` guarda o access token só em memória e o refresh token em `~/.awtrix-hub/tiktok_token.json` (`0o600`); `tiktok_login_state` (`0o600`) | corpo do POST `/v2/oauth/token/` e header `Authorization: Bearer` em `sources/tiktok` e `scripts/tiktok.ax`; no relógio, `@config` + store `tok` (`rt`/`at`), visíveis pela API do relógio |
| `AWTRIX_USER`/`AWTRIX_PASS` | `Device.User/Pass` | `awtrix.(*Client).WithAuth` | HTTP Basic em `(*Client).do`, em texto claro na LAN (o relógio só fala HTTP) |

Para cada linha, confira:

1. **Carga** (`internal/config/config.go`): os campos de segredo têm `yaml:"-"`? Um segredo novo precisa disso, senão alguém o coloca no `config.yaml` e o valor acaba versionado por engano. `loadDotEnv` não pode registrar em log o que leu.
2. **Deploy** (`internal/deploy/deploy.go`): segredo vazio não é enviado (`secret(...)`, regra do CLAUDE.md), e `source` nunca é enviado. Os logs de `Deploy` registram só a contagem de chaves (`"chaves", len(vals)`), nunca `vals`. Erros de `PatchAppConfig` embrulhados em `configurar %s: %w` não podem incluir o corpo enviado.
3. **Uso em URL**: o `*url.Error` do Go traz a URL **inteira** na mensagem (`Get "https://...?key=...": dial tcp ...`). Qualquer `err` de `sources.GetJSON` com segredo na query que chegue a `s.log.Warn("source failed", ..., "err", err)` (`internal/scheduler/scheduler.go`) acaba em stdout e em `~/Library/Logs/awtrix-hub.log` (launchd). O mesmo vale para `HTTPError.Body`, que guarda até 512 bytes da resposta. Confira se a API ecoa o token na mensagem de erro.
4. **Persistência**: `instagram.(*Source).save` grava com `0o600` dentro de um diretório `0o700`. Qualquer arquivo novo com segredo em `data_dir` deve seguir esse padrão. `state.(*Store)` grava `state.json` com `0o644`. Isso só é aceitável enquanto ele guardar apenas contadores. Se aparecer um campo de segredo em `state.Data`, é achado.
5. **No relógio (Berry)**: `log(...)` em `scripts/*.ax` vai para `GET /api/v1/logs`, sem autenticação (a UI não tem senha, por escolha do usuário). Nenhum `log` pode concatenar `st.get("token")`, `st.get("api_key")`, `t["t"]`, URL montada com segredo ou corpo de resposta. Em `scripts/counter.ax` os logs usam `self.key`, `how` e `status`, e qualquer log novo deve seguir esse padrão. `notify(...)` e o texto desenhado em `draw()` também aparecem na tela e em `GET /api/v1/display/screen`.
6. **Metadados do `@config`**: segredo é `text` com `default=""` e `maxlen`. Um `default` com valor real publica o segredo no repositório.

## 3. Superfícies de rede

- **`internal/api` (`api.Handler`)**: sem autenticação, servido por `serveAPI` em `api.addr` (o exemplo usa `0.0.0.0:7080`). Hoje expõe só `value`/`updatedAt`/`age` de `state.Counter`. Se uma mudança passar a servir algo além dos contadores (config, tokens, `state.Data` inteiro), é achado. Confira também se `r.PathValue("key")` é usado só como chave de mapa e nunca como caminho de arquivo.
- **Simulador (`internal/sim/server.go`)**: `logRequest` registra e transmite por SSE (`/events`) o **corpo inteiro** de cada app e notificação. Se algum app ou notificação carregar segredo, ele vaza no navegador. `GET /icons/{id}` passa `id` para `sim.Icons.Get`: verifique a sanitização contra path traversal no cache de ícones (`internal/sim/icons.go`) e que o `id` só monte URL da LaMetric. `readJSON` limita o corpo a 8 KB, e endpoints novos precisam do mesmo limite. O padrão é `localhost:7070`: um `sim.addr` em `0.0.0.0` expõe tudo isso na rede.
- **Servidores HTTP em `cmd/awtrix-hub/main.go`**: precisam ter `ReadHeaderTimeout` (Slowloris). Faltando `ReadTimeout`/`WriteTimeout`, o achado é baixo (SSE precisa ficar aberto).
- **Clientes HTTP**: `sources.HTTPClient` e `awtrix.Client` têm timeout. As leituras usam `io.LimitReader`, e não pode haver `io.ReadAll` sem limite em resposta externa. `json.NewDecoder(resp.Body)` sem limite em `GetJSON` é aceitável para APIs conhecidas, mas registre se surgir uma fonte arbitrária.
- **SSRF/redirecionamento**: `device.url`, `targets`, `api.public_url` e `hub_url` vêm de config local, não de input remoto. Só vira achado se um valor vindo da rede (resposta de API, payload do relógio) decidir para onde o hub manda credenciais. O `http.Client` padrão segue redirects, e o Go remove `Authorization` em redirect para outro host, mas **não** remove segredo da query string. Verifique isso para YouTube e Instagram.
- **TLS**: chamadas para GitHub, Google, Instagram e ESPN devem ser `https://`. Um `InsecureSkipVerify` ou uma URL `http://` para serviço externo é achado alto.

## 4. Repositório e CI

- `.gitignore` cobre `.env` e `config.yaml`. Arquivos de teste (`testdata/`, `*_test.go`, `scripts/test/`) só podem ter valores falsos óbvios (`"x"`, `"tok"`).
- `.env.example` e `config.example.yaml` não podem ter valores reais. O IP do relógio na LAN não é segredo, mas não deve ir para o repositório.
- `.github/workflows/*` (quando existirem):
  - `permissions:` mínimo (`contents: read`);
  - nada de `pull_request_target` com checkout do código da PR;
  - actions de terceiros fixadas por SHA;
  - `secrets.*` nunca passados a passos que rodam código da PR;
  - nada de `${{ github.event.* }}` interpolado direto em `run:` (injeção de shell).
- Hooks e scripts versionados (`.githooks/`, `Makefile`) não podem enviar dados para fora nem ler `.env`.

## 5. Formato do relatório (em português)

Ordene por severidade: **Crítica** (segredo já exposto ou exposto sem interação), **Alta** (exposto em log, URL ou rede local sem auth em uso normal), **Média** (exige condição plausível: erro de rede, config alternativa), **Baixa** (endurecimento), **Info**.

Para cada achado:

```
[Alta] Token do Instagram em logs do hub em falha de rede
Onde: internal/sources/instagram/instagram.go:57 → internal/scheduler/scheduler.go:143
Cenário: DNS falha → *url.Error com "...?access_token=<token>" → s.log.Warn → ~/Library/Logs/awtrix-hub.log
Correção: remover a query do erro (ex.: embrulhar err sem a URL) ou mandar o token no header quando a API aceitar
```

No fim, inclua uma linha com o resultado do gitleaks e do govulncheck e a lista das superfícies verificadas sem achado, para que fique claro o que foi coberto. Se o escopo for `diff` e nada relevante tiver mudado, diga isso em uma linha em vez de repetir a auditoria completa.
